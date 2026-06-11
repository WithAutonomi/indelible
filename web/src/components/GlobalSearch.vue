<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api/client'
import { useAuthStore } from '../stores/auth'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import ToggleSwitch from 'primevue/toggleswitch'

const auth = useAuthStore()
const router = useRouter()

const open = ref(false)
const query = ref('')
const everything = ref(false)
const loading = ref(false)
const activeIndex = ref(0)
const inputEl = ref<{ $el: HTMLInputElement } | null>(null)

type Hit = { type: string; id: string; label: string; sublabel?: string }
const results = ref<Record<string, Hit[]>>({})

// Client-side page/nav targets, so the palette doubles as a command bar.
const PAGES: Array<{ label: string; path: string; admin?: boolean }> = [
  { label: 'Dashboard', path: '/' },
  { label: 'Uploads', path: '/uploads' },
  { label: 'Collections', path: '/collections' },
  { label: 'API Tokens', path: '/tokens' },
  { label: 'Users', path: '/admin/users', admin: true },
  { label: 'Groups', path: '/admin/groups', admin: true },
  { label: 'Wallets', path: '/admin/wallets', admin: true },
  { label: 'Transactions', path: '/admin/transactions', admin: true },
  { label: 'Quotas', path: '/admin/quotas', admin: true },
  { label: 'Tag Rules', path: '/admin/tag-rules', admin: true },
  { label: 'Webhooks', path: '/admin/webhooks', admin: true },
  { label: 'SSO', path: '/admin/sso', admin: true },
  { label: 'SCIM', path: '/admin/scim', admin: true },
  { label: 'Settings', path: '/admin/settings', admin: true },
  { label: 'Analytics', path: '/admin/analytics', admin: true },
  { label: 'Logs', path: '/admin/logs', admin: true },
  { label: 'System', path: '/admin/system', admin: true },
]

// Fallback section per result type, for hits we don't deep-link yet (no detail
// drawer on the target page).
const TYPE_ROUTE: Record<string, string> = {
  file: '/uploads',
  collection: '/collections',
  tag: '/uploads',
  user: '/admin/users',
  token: '/tokens',
  webhook: '/admin/webhooks',
}

// Resolve a hit to its destination. Where the target page has a detail drawer
// (files, users) we deep-link with ?focus=<id> so the page opens that record;
// a tag hit pre-applies the uploads tag filter. Everything else falls back to
// the section route (V2-459 extends as more pages gain detail drawers).
function hitTarget(hit: Hit): { path: string; query?: Record<string, string> } | null {
  switch (hit.type) {
    case 'file':
      return { path: '/uploads', query: { focus: hit.id } }
    case 'user':
      return { path: '/admin/users', query: { focus: hit.id } }
    case 'tag': {
      // id is "key=value" — split on the first '=' (values may contain '=').
      const eq = hit.id.indexOf('=')
      if (eq > 0) return { path: '/uploads', query: { tagKey: hit.id.slice(0, eq), tagValue: hit.id.slice(eq + 1) } }
      return { path: '/uploads' }
    }
    default: {
      const p = TYPE_ROUTE[hit.type]
      return p ? { path: p } : null
    }
  }
}

const GROUPS = [
  { key: 'files', label: 'Files', icon: 'pi pi-file' },
  { key: 'collections', label: 'Collections', icon: 'pi pi-folder' },
  { key: 'tags', label: 'Tags', icon: 'pi pi-tags' },
  { key: 'users', label: 'Users', icon: 'pi pi-users' },
  { key: 'tokens', label: 'API Tokens', icon: 'pi pi-key' },
  { key: 'webhooks', label: 'Webhooks', icon: 'pi pi-bell' },
]

const pageHits = computed(() => {
  const q = query.value.trim().toLowerCase()
  if (!q) return []
  return PAGES.filter(p => (!p.admin || auth.isAdmin) && p.label.toLowerCase().includes(q))
})

type FlatItem =
  | { kind: 'page'; path: string; label: string }
  | { kind: 'hit'; hit: Hit }

// Ordered, selectable list (pages first, then backend groups) for keyboard nav.
const flat = computed<FlatItem[]>(() => {
  const items: FlatItem[] = []
  for (const p of pageHits.value) items.push({ kind: 'page', path: p.path, label: p.label })
  for (const g of GROUPS) for (const h of results.value[g.key] || []) items.push({ kind: 'hit', hit: h })
  return items
})

// Renderable list with group headers; each item carries its flat index so the
// highlight stays in sync with keyboard navigation.
const display = computed(() => {
  const out: Array<
    | { kind: 'header'; label: string; icon: string }
    | { kind: 'item'; item: FlatItem; index: number }
  > = []
  let idx = 0
  if (pageHits.value.length) {
    out.push({ kind: 'header', label: 'Pages', icon: 'pi pi-compass' })
    for (const p of pageHits.value) out.push({ kind: 'item', item: { kind: 'page', path: p.path, label: p.label }, index: idx++ })
  }
  for (const g of GROUPS) {
    const hits = results.value[g.key] || []
    if (!hits.length) continue
    out.push({ kind: 'header', label: g.label, icon: g.icon })
    for (const h of hits) out.push({ kind: 'item', item: { kind: 'hit', hit: h }, index: idx++ })
  }
  return out
})

const hasResults = computed(() => flat.value.length > 0)
const tooShort = computed(() => query.value.trim().length > 0 && query.value.trim().length < 2)

let timer: ReturnType<typeof setTimeout> | null = null
watch([query, everything], () => {
  if (timer) clearTimeout(timer)
  timer = setTimeout(runSearch, 250)
})

async function runSearch() {
  activeIndex.value = 0
  const q = query.value.trim()
  if (q.length < 2) {
    results.value = {}
    return
  }
  loading.value = true
  try {
    const params: any = { q }
    if (everything.value && auth.isAdmin) params.scope = 'all'
    const res = await api.get('/api/v2/search', { params })
    results.value = res.data || {}
  } catch {
    results.value = {}
  } finally {
    loading.value = false
  }
}

function activate(item: FlatItem) {
  if (item.kind === 'page') {
    router.push(item.path)
  } else {
    const target = hitTarget(item.hit)
    if (target) router.push(target)
  }
  close()
}

function openPalette() {
  query.value = ''
  results.value = {}
  activeIndex.value = 0
  open.value = true
}
function close() {
  open.value = false
}

function focusInput() {
  inputEl.value?.$el?.focus()
}

function onKeydown(e: KeyboardEvent) {
  if (!hasResults.value) return
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    activeIndex.value = (activeIndex.value + 1) % flat.value.length
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    activeIndex.value = (activeIndex.value - 1 + flat.value.length) % flat.value.length
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const it = flat.value[activeIndex.value]
    if (it) activate(it)
  }
}

// Global Cmd/Ctrl+K to open.
function onGlobalKey(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
    e.preventDefault()
    openPalette()
  }
}
onMounted(() => window.addEventListener('keydown', onGlobalKey))
onUnmounted(() => window.removeEventListener('keydown', onGlobalKey))
</script>

<template>
  <!-- Sidebar trigger styled as a faux search field -->
  <button
    type="button"
    class="flex items-center gap-2 w-full px-3 py-2 rounded-lg text-sm text-surface-500 bg-surface-100 hover:bg-surface-200 transition-colors"
    @click="openPalette"
  >
    <i class="pi pi-search text-xs"></i>
    <span class="flex-1 text-left">Search…</span>
    <kbd class="text-[10px] px-1.5 py-0.5 rounded border border-surface-300 text-surface-400">Ctrl K</kbd>
  </button>

  <Dialog
    v-model:visible="open"
    modal
    :showHeader="false"
    :dismissableMask="true"
    class="w-full max-w-xl"
    @show="focusInput"
  >
    <div class="flex items-center gap-2 border-b border-surface-200 pb-3 mb-2">
      <i class="pi pi-search text-surface-400"></i>
      <InputText
        ref="inputEl"
        v-model="query"
        placeholder="Search files, collections, tags…"
        class="flex-1 border-0 shadow-none focus:ring-0 px-0"
        autocomplete="off"
        @keydown="onKeydown"
      />
      <div v-if="auth.isAdmin" class="flex items-center gap-2 shrink-0" v-tooltip.bottom="'Include users, API tokens and webhooks'">
        <span class="text-xs text-surface-500">Everything</span>
        <ToggleSwitch v-model="everything" />
      </div>
    </div>

    <div class="max-h-96 overflow-auto -mx-1">
      <template v-for="(row, i) in display" :key="i">
        <div v-if="row.kind === 'header'" class="flex items-center gap-2 px-2 pt-3 pb-1 text-[11px] uppercase tracking-wide text-surface-400 font-medium">
          <i :class="row.icon" class="text-[11px]"></i>{{ row.label }}
        </div>
        <button
          v-else
          type="button"
          class="flex items-center gap-2 w-full px-2 py-2 rounded-md text-left text-sm"
          :class="row.index === activeIndex ? 'bg-primary/10 text-primary' : 'text-surface-700 hover:bg-surface-100'"
          @click="activate(row.item)"
          @mouseenter="activeIndex = row.index"
        >
          <span class="truncate">{{ row.item.kind === 'page' ? row.item.label : row.item.hit.label }}</span>
          <span v-if="row.item.kind === 'hit' && row.item.hit.sublabel"
            class="ml-auto pl-3 text-xs text-surface-400 truncate shrink-0 max-w-[40%]">
            {{ row.item.hit.sublabel }}
          </span>
          <span v-else-if="row.item.kind === 'page'" class="ml-auto text-xs text-surface-400">Go to</span>
        </button>
      </template>

      <div v-if="tooShort" class="px-2 py-6 text-center text-sm text-surface-400">Keep typing…</div>
      <div v-else-if="query.trim().length >= 2 && !hasResults && !loading" class="px-2 py-6 text-center text-sm text-surface-400">
        No matches for “{{ query.trim() }}”.
      </div>
      <div v-else-if="!query.trim()" class="px-2 py-6 text-center text-sm text-surface-400">
        Search files, collections and tags.
        <span v-if="auth.isAdmin">Toggle <b>Everything</b> for users, tokens and webhooks.</span>
      </div>
    </div>
  </Dialog>
</template>
