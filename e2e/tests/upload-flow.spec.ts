import { test, expect, request as playwrightRequest } from '@playwright/test'
import { adminHeaders } from '../helpers/auth'
import crypto from 'crypto'

// Upload → list → download → verify content. The canonical "does the whole
// thing work" E2E test: hits the real upload endpoint, waits for the worker
// to push the file through antd onto the Autonomi network, then re-fetches
// it and asserts the bytes match.
//
// Preconditions checked at run time (test self-skips when missing):
//   1. /health reports antd OK — antd is running and has peers.
//   2. /api/v2/system/wallet-status reports a default wallet, OR we can
//      create one with the well-known TEST_WALLET_KEY (devnet only).
//
// Wallet key reused from smoke/helpers/fixtures.ts so dev1's devnet setup
// only needs to fund this one address once.
const TEST_WALLET_KEY = 'ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80'

interface HealthBody {
  status: string
  antd?: boolean
  database?: boolean
}

async function getHealth(): Promise<HealthBody> {
  const ctx = await playwrightRequest.newContext({ baseURL: 'http://localhost:8080' })
  const res = await ctx.get('/health')
  const body = (await res.json()) as HealthBody
  await ctx.dispose()
  return body
}

test.describe('Real E2E: upload → list → download', () => {
  test.beforeAll(async () => {
    const health = await getHealth()
    if (!health.database) {
      test.skip(true, `Indelible /health reports database not ok: ${JSON.stringify(health)}`)
    }
    if (!health.antd) {
      test.skip(true, `antd not reachable (health.antd=false) — start antd or point INDELIBLE_ANTD_URL at a running instance`)
    }
  })

  test('admin uploads a small file, sees it in the list, downloads it, content matches', async ({ request }) => {
    const headers = adminHeaders()

    // 1) Ensure a default wallet exists. POST creates one with the well-known
    //    devnet key; the handler returns 201 on success or fails clearly on
    //    payment/network errors so we see why.
    const statusRes = await request.get('/api/v2/system/wallet-status', { headers })
    expect(statusRes.status()).toBe(200)
    const status = await statusRes.json()
    if (!status.has_default_wallet) {
      const createRes = await request.post('/api/v2/admin/wallets', {
        headers,
        data: { name: `e2e-primary-${Date.now()}`, private_key: TEST_WALLET_KEY },
      })
      if (createRes.status() !== 201) {
        test.skip(true, `wallet create failed (${createRes.status()}): ${await createRes.text()}`)
      }
    }

    // 2) Build a small file with a known payload. crypto.randomBytes ensures
    //    we'd notice silent truncation / "all uploads return the same chunk"
    //    bugs that a fixed string wouldn't catch.
    const payload = crypto.randomBytes(4096)
    const filename = `e2e-${Date.now()}.bin`

    // Playwright's multipart helper handles boundary + content-disposition.
    const uploadRes = await request.post('/api/v2/uploads', {
      headers,
      multipart: {
        file: { name: filename, mimeType: 'application/octet-stream', buffer: payload },
        visibility: 'private',
      },
    })
    expect(uploadRes.status(), `upload POST: ${await uploadRes.text()}`).toBe(202)
    const uploadBody = await uploadRes.json()
    const uploadID = uploadBody.upload.id as number
    expect(uploadID).toBeGreaterThan(0)

    // 3) Poll until the worker reports completed. Test timeout is the outer
    //    guard (120s in playwright.config); inside we poll every 2s and bail
    //    early on terminal failure so we see the real error.
    let lastStatus = ''
    let lastUpload: any = null
    const deadline = Date.now() + 90_000
    while (Date.now() < deadline) {
      const getRes = await request.get(`/api/v2/uploads/${uploadID}`, { headers })
      expect(getRes.status()).toBe(200)
      lastUpload = await getRes.json()
      lastStatus = lastUpload.status
      if (lastStatus === 'completed') break
      if (lastStatus === 'failed' || lastStatus === 'cancelled') {
        test.fail(true, `upload reached terminal state ${lastStatus} — error: ${lastUpload.error || '(none reported)'}`)
        return
      }
      await new Promise((r) => setTimeout(r, 2000))
    }
    expect(lastStatus, `final upload state after 90s: ${JSON.stringify(lastUpload)}`).toBe('completed')

    // 4) Listing should include this upload. Cursor pagination defaults to
    //    newest first, so it should be in the first page.
    const listRes = await request.get('/api/v2/uploads?limit=20', { headers })
    expect(listRes.status()).toBe(200)
    const list = await listRes.json()
    const found = (list.uploads as any[]).find((u) => u.id === uploadID)
    expect(found, `upload ${uploadID} not in /uploads list (got: ${(list.uploads as any[]).map((u: any) => u.id).join(',')})`).toBeTruthy()
    expect(found.original_filename).toBe(filename)

    // 5) Download and compare bytes. Playwright's request.get returns a Body
    //    we can read into a buffer; equality is checked at the byte level.
    const dlRes = await request.get(`/api/v2/uploads/${uploadID}/download`, { headers })
    expect(dlRes.status(), `download: ${await dlRes.text()}`).toBe(200)
    const downloaded = await dlRes.body()
    expect(downloaded.length, 'downloaded byte count').toBe(payload.length)
    expect(Buffer.compare(downloaded, payload), 'downloaded bytes diverge from uploaded bytes').toBe(0)
  })
})
