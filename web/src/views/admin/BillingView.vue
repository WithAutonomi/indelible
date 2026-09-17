<script setup lang="ts">
// Billing (V2-1097): the hosted-mode home for funds — gateway credits, card
// top-ups via the server-side relay (the tenant key never reaches the
// browser), and the credited top-up history. Local-signing instances are
// redirected to Wallets; hosted instances land here from the Wallets nav slot.
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
import { attoToANT, approxUSD, fmtUSD, gbRemaining, costPerGBTooltip, type CostPerGBBasis } from '../../utils/money'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Button from 'primevue/button'
import InputNumber from 'primevue/inputnumber'
import Message from 'primevue/message'
import Dialog from 'primevue/dialog'

const route = useRoute()
const router = useRouter()
const toast = useToast()

const loading = ref(true)
const gatewayUrl = ref('')
const creditAtto = ref<string | null>(null)
// Crypto-free counter (V2-1100): with a gateway rate, everything renders in
// fiat; the ANT figures live in tooltips (crypto-lite underneath).
const rate = ref('')
// Storage-capacity estimate (V2-1114): the gateway's directional cost-per-GB
// plus its methodology basis. Absent (older gateway, thin paid history) hides
// the "≈ N GB remaining" line entirely — absent means "not enough data yet".
const estCostPerGB = ref('')
const estBasis = ref<CostPerGBBasis | null>(null)
type Credit = { id: number; source: 'card' | 'grant'; amount_atto: string; amount_usd_cents: number; note: string; session_id: string; created_at: string }
const credits = ref<Credit[]>([])

// Card rows carry their exact locked USD; grants convert at the current
// rate (≈) with the ANT fallback when no rate is available.
function creditAmount(c: Credit): string {
  if (c.source === 'card' && c.amount_usd_cents > 0) return fmtUSD(c.amount_usd_cents)
  return approxUSD(c.amount_atto, rate.value) ?? `${attoToANT(c.amount_atto)} ANT`
}

const LOW_CREDIT_ATTO = 10n ** 18n // 1 ANT — a handful of uploads' headroom
const creditsLow = () => {
  if (creditAtto.value === null) return false
  try { return BigInt(creditAtto.value) < LOW_CREDIT_ATTO } catch { return false }
}

const creditUSD = () => approxUSD(creditAtto.value, rate.value)

// "≈ N GB remaining at current prices" (V2-1106's secondary line): balance ÷
// cost-per-GB, one decimal, floored. Null hides the line.
const gbLeft = () => gbRemaining(creditAtto.value, estCostPerGB.value)

async function fetchBilling() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/billing')
    gatewayUrl.value = res.data.payment_gateway_url || ''
    creditAtto.value = res.data.gateway_credit_atto ?? null
    rate.value = res.data.rate_usd_per_ant || ''
    estCostPerGB.value = res.data.est_cost_per_gb_atto || ''
    estBasis.value = res.data.est_cost_per_gb_basis || null
    credits.value = res.data.credits || []
  } catch (e: any) {
    if (e.response?.status === 400) {
      // Not in hosted mode — billing has no meaning; Wallets is the home.
      router.replace('/admin/wallets')
      return
    }
  } finally {
    loading.value = false
  }
}

// ── top-up flow: amount → relay creates the Checkout session → confirm the
// exact credit → hand the browser to Stripe. Stripe lands back here with
// ?topup=<session id> (or ?cancelled=1).
const amountUSD = ref<number | null>(25)
const startingTopup = ref(false)
const confirmVisible = ref(false)
const pendingSession = ref<{ session_id: string; url: string; credit_atto: string; amount_usd_cents: number } | null>(null)

async function startTopup() {
  if (!amountUSD.value || amountUSD.value <= 0) return
  startingTopup.value = true
  try {
    const origin = window.location.origin
    const res = await api.post('/api/v2/admin/billing/topup-checkout', {
      amount_usd_cents: Math.round(amountUSD.value * 100),
      success_url: `${origin}/admin/billing?topup={CHECKOUT_SESSION_ID}`,
      cancel_url: `${origin}/admin/billing?cancelled=1`,
    })
    pendingSession.value = res.data
    confirmVisible.value = true
  } catch (e: any) {
    toast.add({ severity: 'error', summary: 'Top-up failed', detail: e.response?.data?.error || 'Could not start the top-up', life: 6000 })
  } finally {
    startingTopup.value = false
  }
}

function continueToPayment() {
  if (pendingSession.value) window.location.href = pendingSession.value.url
}

// On return from Stripe, ask the gateway to sync the session — the
// deterministic fallback that credits even if the webhook was lost.
// Idempotent with webhook delivery, so racing it is safe.
async function syncReturn(sessionID: string) {
  try {
    const res = await api.post('/api/v2/admin/billing/topup-sync', { session_id: sessionID })
    if (res.data.credited) {
      const usd = approxUSD(res.data.credit_atto, rate.value)
      toast.add({ severity: 'success', summary: 'Top-up received', detail: usd ? `${usd.replace('≈ ', '')} of storage credit added` : `Credited ${attoToANT(res.data.credit_atto)} ANT`, life: 6000 })
    } else {
      toast.add({ severity: 'info', summary: 'Payment pending', detail: `Stripe reports ${res.data.payment_status}; credits land when the payment completes.`, life: 6000 })
    }
  } catch (e: any) {
    toast.add({ severity: 'warn', summary: 'Sync failed', detail: e.response?.data?.error || 'Could not confirm the payment yet — credits still land via the gateway webhook.', life: 6000 })
  }
}

onMounted(async () => {
  const sessionID = route.query.topup
  const cancelled = route.query.cancelled
  if (sessionID || cancelled) {
    // Strip the params so refresh doesn't replay the toast.
    router.replace({ path: '/admin/billing' })
  }
  // First fetch loads the rate so the sync toast can speak fiat; the second
  // picks up the freshly-credited balance.
  await fetchBilling()
  if (typeof sessionID === 'string' && sessionID !== '') {
    await syncReturn(sessionID)
    await fetchBilling()
  } else if (cancelled) {
    toast.add({ severity: 'info', summary: 'Top-up cancelled', detail: 'No payment was taken.', life: 4000 })
  }
})
</script>

<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-2xl font-bold">Billing</h1>
      <Button icon="pi pi-refresh" label="Refresh" severity="secondary" text @click="fetchBilling" :loading="loading" />
    </div>

    <Message :severity="creditsLow() ? 'error' : 'info'" :closable="false" class="mb-6">
      <div>
        <p class="font-medium">Hosted payment mode — uploads are paid by the payment gateway from prepaid credits</p>
        <p v-if="creditsLow()" class="text-sm font-medium mt-1">Low balance: uploads will start failing; top up below.</p>
        <p v-if="gatewayUrl" class="text-sm">Gateway: <code>{{ gatewayUrl }}</code></p>
      </div>
    </Message>

    <div class="grid grid-cols-1 md:grid-cols-2 gap-6 mb-6">
      <!-- Credits: fiat at the counter, exact ANT in the tooltip -->
      <div class="rounded-lg border border-surface-200 bg-surface-0 p-5">
        <p class="text-sm text-surface-500 mb-1">Remaining storage credit</p>
        <p class="text-3xl font-bold" :class="creditsLow() ? 'text-red-600' : ''">
          <span v-if="creditAtto !== null && creditUSD()" :title="`${attoToANT(creditAtto)} ANT`">{{ creditUSD() }}</span>
          <span v-else-if="creditAtto !== null">{{ attoToANT(creditAtto) }} <span class="text-base font-medium text-surface-400">ANT</span></span>
          <span v-else class="text-surface-400 text-xl">unavailable</span>
        </p>
        <p v-if="creditAtto === null && !loading" class="text-xs text-surface-400 mt-1">The gateway could not be reached; credits are unaffected.</p>
        <!-- Capacity estimate (V2-1114): directional by design — the tooltip
             carries the gateway's own methodology; hidden entirely when the
             gateway has no estimate yet. -->
        <p v-if="gbLeft()" class="text-sm text-surface-500 mt-1 cursor-help"
          :title="costPerGBTooltip(estBasis) ?? undefined">
          ≈ {{ gbLeft() }} GB remaining at current prices
        </p>
      </div>

      <!-- Top up -->
      <div class="rounded-lg border border-surface-200 bg-surface-0 p-5">
        <p class="text-sm text-surface-500 mb-2">Add funds with a card</p>
        <div class="flex items-end gap-3">
          <div>
            <label class="text-xs text-surface-400 block mb-1">Amount (USD)</label>
            <InputNumber v-model="amountUSD" mode="currency" currency="USD" locale="en-US"
              :min="5" :max="10000" :maxFractionDigits="2" inputClass="w-36" />
          </div>
          <Button icon="pi pi-credit-card" :label="startingTopup ? 'Preparing…' : 'Top up with card'"
            :loading="startingTopup" @click="startTopup" />
        </div>
        <p class="text-xs text-surface-400 mt-2">$5–$10,000 per top-up. You'll confirm the exact ANT credit before paying.</p>
      </div>
    </div>

    <!-- Exact-credit confirmation before handing off to Stripe -->
    <Dialog v-model:visible="confirmVisible" header="Confirm top-up" modal :style="{ width: '26rem' }">
      <div v-if="pendingSession" class="space-y-2">
        <p>You will be charged <span class="font-semibold">{{ fmtUSD(pendingSession.amount_usd_cents) }}</span>
          and receive <span class="font-semibold" :title="`exactly ${attoToANT(pendingSession.credit_atto)} ANT`">{{ fmtUSD(pendingSession.amount_usd_cents) }} of storage credit</span>.</p>
        <p class="text-sm text-surface-500">Payment is handled by Stripe. Credit lands automatically once the payment completes.</p>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="confirmVisible = false" />
        <Button label="Continue to payment" icon="pi pi-external-link" @click="continueToPayment" />
      </template>
    </Dialog>

    <!-- Credit history: every funding path — card top-ups and invoice grants -->
    <h2 class="text-lg font-semibold mb-3">Credit history</h2>
    <DataTable :value="credits" :loading="loading" stripedRows class="rounded-lg border border-surface-200 mb-4"
      :pt="{ root: { class: 'bg-surface-0' } }">
      <template #empty>No credit has been added yet.</template>
      <Column field="created_at" header="Time" sortable>
        <template #body="{ data }">
          <span class="text-surface-500 whitespace-nowrap">{{ new Date(data.created_at).toLocaleString() }}</span>
        </template>
      </Column>
      <Column field="amount_usd_cents" header="Credited" sortable>
        <template #body="{ data }">
          <span class="font-medium" :title="`${attoToANT(data.amount_atto)} ANT`">{{ creditAmount(data) }}</span>
        </template>
      </Column>
      <Column field="source" header="Source">
        <template #body="{ data }">
          <span class="inline-flex items-center gap-2">
            <span class="text-xs font-medium px-2 py-0.5 rounded"
              :class="data.source === 'card' ? 'bg-primary-100 text-primary-700' : 'bg-surface-100 text-surface-600'">
              {{ data.source === 'card' ? 'Card' : 'Invoice / grant' }}
            </span>
            <!-- card rows' notes just repeat the session reference -->
            <span v-if="data.note && data.source !== 'card'" class="text-xs text-surface-400">{{ data.note }}</span>
          </span>
        </template>
      </Column>
      <Column field="session_id" header="Reference">
        <template #body="{ data }">
          <code v-if="data.session_id" class="text-xs text-surface-400" :title="data.session_id">{{ data.session_id.slice(0, 18) }}…</code>
          <span v-else class="text-xs text-surface-300">—</span>
        </template>
      </Column>
    </DataTable>

    <router-link to="/admin/transactions?type=hosted_payment" class="text-sm text-primary font-medium hover:underline">
      View hosted payments in Transactions →
    </router-link>
  </div>
</template>
