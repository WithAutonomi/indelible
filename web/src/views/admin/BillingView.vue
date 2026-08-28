<script setup lang="ts">
// Billing (V2-1097): the hosted-mode home for funds — gateway credits, card
// top-ups via the server-side relay (the tenant key never reaches the
// browser), and the credited top-up history. Local-signing instances are
// redirected to Wallets; hosted instances land here from the Wallets nav slot.
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useToast } from 'primevue/usetoast'
import { api } from '../../api/client'
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
type Topup = { id: number; session_id: string; amount_usd_cents: number; credit_atto: string; created_at: string }
const topups = ref<Topup[]>([])

// Atto → ANT for display: BigInt string math, no floats.
function attoToANT(atto: string): string {
  const s = atto.padStart(19, '0')
  const whole = s.slice(0, -18)
  const frac = s.slice(-18).replace(/0+$/, '')
  return frac ? `${whole}.${frac}` : whole
}

const LOW_CREDIT_ATTO = 10n ** 18n // 1 ANT — a handful of uploads' headroom
const creditsLow = () => {
  if (creditAtto.value === null) return false
  try { return BigInt(creditAtto.value) < LOW_CREDIT_ATTO } catch { return false }
}

function fmtUSD(cents: number): string {
  return `$${(cents / 100).toFixed(2)}`
}

async function fetchBilling() {
  loading.value = true
  try {
    const res = await api.get('/api/v2/admin/billing')
    gatewayUrl.value = res.data.payment_gateway_url || ''
    creditAtto.value = res.data.gateway_credit_atto ?? null
    topups.value = res.data.topups || []
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
      toast.add({ severity: 'success', summary: 'Top-up received', detail: `Credited ${attoToANT(res.data.credit_atto)} ANT`, life: 6000 })
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
  if (typeof sessionID === 'string' && sessionID !== '') {
    await syncReturn(sessionID)
  } else if (cancelled) {
    toast.add({ severity: 'info', summary: 'Top-up cancelled', detail: 'No payment was taken.', life: 4000 })
  }
  await fetchBilling()
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
      <!-- Credits -->
      <div class="rounded-lg border border-surface-200 bg-surface-0 p-5">
        <p class="text-sm text-surface-500 mb-1">Remaining credits</p>
        <p class="text-3xl font-bold" :class="creditsLow() ? 'text-red-600' : ''">
          <span v-if="creditAtto !== null">{{ attoToANT(creditAtto) }} <span class="text-base font-medium text-surface-400">ANT</span></span>
          <span v-else class="text-surface-400 text-xl">unavailable</span>
        </p>
        <p v-if="creditAtto === null && !loading" class="text-xs text-surface-400 mt-1">The gateway could not be reached; credits are unaffected.</p>
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
          and credited exactly <span class="font-semibold">{{ attoToANT(pendingSession.credit_atto) }} ANT</span>.</p>
        <p class="text-sm text-surface-500">Payment is handled by Stripe. Credits land automatically once the payment completes.</p>
      </div>
      <template #footer>
        <Button label="Cancel" severity="secondary" text @click="confirmVisible = false" />
        <Button label="Continue to payment" icon="pi pi-external-link" @click="continueToPayment" />
      </template>
    </Dialog>

    <!-- Top-up history -->
    <h2 class="text-lg font-semibold mb-3">Top-up history</h2>
    <DataTable :value="topups" :loading="loading" stripedRows class="rounded-lg border border-surface-200 mb-4"
      :pt="{ root: { class: 'bg-surface-0' } }">
      <template #empty>No card top-ups yet.</template>
      <Column field="created_at" header="Time" sortable>
        <template #body="{ data }">
          <span class="text-surface-500 whitespace-nowrap">{{ new Date(data.created_at).toLocaleString() }}</span>
        </template>
      </Column>
      <Column field="amount_usd_cents" header="Paid" sortable>
        <template #body="{ data }">
          <span class="font-medium">{{ fmtUSD(data.amount_usd_cents) }}</span>
        </template>
      </Column>
      <Column field="credit_atto" header="Credited (ANT)" sortable>
        <template #body="{ data }">
          <span class="font-mono">{{ attoToANT(data.credit_atto) }}</span>
        </template>
      </Column>
      <Column field="session_id" header="Reference">
        <template #body="{ data }">
          <code class="text-xs text-surface-400" :title="data.session_id">{{ data.session_id.slice(0, 18) }}…</code>
        </template>
      </Column>
    </DataTable>

    <router-link to="/admin/transactions?type=hosted_payment" class="text-sm text-primary font-medium hover:underline">
      View hosted payments in Transactions →
    </router-link>
  </div>
</template>
