<template>
  <div
    class="rounded-xl border border-orange-200 bg-orange-50/60 p-4 dark:border-orange-800/60 dark:bg-orange-900/10"
    data-testid="account-mailbox-panel"
  >
    <h4 class="mb-3 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ title }}</h4>

    <div class="grid grid-cols-1 gap-x-3 gap-y-3 sm:grid-cols-2">
      <div>
        <label class="input-label">{{ t('admin.accounts.mailbox.host') }}</label>
        <input v-model.trim="config.host" type="text" class="input w-full" placeholder="imap.gmail.com" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.accounts.mailbox.port') }}</label>
        <input v-model.number="config.port" type="number" min="1" max="65535" class="input w-full" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.accounts.mailbox.username') }}</label>
        <input
          v-model.trim="config.username"
          type="email"
          autocomplete="off"
          class="input w-full"
          placeholder="name@gmail.com"
        />
      </div>
      <div>
        <label class="input-label">{{ t('admin.accounts.mailbox.password') }}</label>
        <input
          v-model="config.password"
          type="password"
          autocomplete="new-password"
          class="input w-full"
          :placeholder="hasSavedPassword ? t('admin.accounts.mailbox.passwordSavedPlaceholder') : ''"
        />
      </div>
      <div class="sm:col-span-2">
        <label class="input-label">{{ t('admin.accounts.mailbox.folder') }}</label>
        <input v-model.trim="config.folder" type="text" class="input w-full" placeholder="INBOX" />
      </div>
    </div>

    <div class="mt-3 flex flex-wrap items-center gap-3">
      <button
        type="button"
        class="inline-flex items-center rounded-lg bg-orange-500 px-3 py-2 text-sm font-semibold text-white hover:bg-orange-600 disabled:cursor-not-allowed disabled:opacity-60"
        :disabled="loading"
        data-testid="mailbox-pull-button"
        @click="pull"
      >
        <Icon v-if="loading" name="refresh" size="sm" class="mr-1.5 animate-spin" />
        {{ loading ? t('admin.accounts.mailbox.pulling') : t('admin.accounts.mailbox.pull') }}
      </button>
      <button
        v-if="accountId"
        type="button"
        class="btn btn-secondary btn-sm"
        :disabled="saving"
        @click="save"
      >
        {{ t('admin.accounts.mailbox.saveToAccount') }}
      </button>
      <span class="text-xs text-gray-500 dark:text-gray-400">{{ hint }}</span>
    </div>

    <div
      v-if="error"
      class="mt-3 rounded-lg border border-red-200 bg-red-50 p-2.5 text-sm text-red-600 dark:border-red-700 dark:bg-red-900/30 dark:text-red-400"
    >
      {{ error }}
    </div>

    <MailboxMessageList v-if="inbox" class="mt-3" :inbox="inbox" :since-minutes="sinceMinutes" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { MailboxConfig, MailboxInboxResult } from '@/api/admin/accounts'
import type { Account } from '@/types'
import MailboxMessageList from './MailboxMessageList.vue'
import { saveAccountMailbox } from './mailbox'

const props = withDefaults(
  defineProps<{
    /** Platform label used in the title, e.g. "Claude". */
    platformLabel?: string
    /** Existing account (re-auth / view mailbox): enables saved-password reuse and "save to account". */
    accountId?: number | null
    hasSavedPassword?: boolean
    sinceMinutes?: number
    autoPull?: boolean
  }>(),
  { platformLabel: '', accountId: null, hasSavedPassword: false, sinceMinutes: 60, autoPull: false }
)

const emit = defineEmits<{ saved: [account: Account] }>()
const config = defineModel<MailboxConfig>({ required: true })

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const inbox = ref<MailboxInboxResult | null>(null)

const title = computed(() =>
  props.platformLabel
    ? t('admin.accounts.mailbox.titleWithPlatform', { platform: props.platformLabel })
    : t('admin.accounts.mailbox.title')
)
const hint = computed(() =>
  props.accountId ? t('admin.accounts.mailbox.existingHint') : t('admin.accounts.mailbox.saveHint')
)

const canUseSavedPassword = computed(
  () => !config.value.password && !!props.accountId && props.hasSavedPassword
)

async function pull() {
  error.value = ''
  const c = config.value
  if (!c.host || !c.username || (!c.password && !canUseSavedPassword.value)) {
    error.value = t('admin.accounts.mailbox.missingFields')
    return
  }
  loading.value = true
  try {
    inbox.value = await adminAPI.accounts.listIMAPInbox({
      account_id: canUseSavedPassword.value ? props.accountId ?? undefined : undefined,
      host: c.host,
      port: c.port || 993,
      username: c.username,
      password: c.password || undefined,
      folder: c.folder || 'INBOX',
      since_minutes: props.sinceMinutes
    })
  } catch (err: unknown) {
    inbox.value = null
    error.value = errorMessage(err)
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!props.accountId) return
  const c = config.value
  if (!c.host || !c.username || (!c.password && !props.hasSavedPassword)) {
    error.value = t('admin.accounts.mailbox.missingFields')
    return
  }
  saving.value = true
  error.value = ''
  try {
    const updated = await saveAccountMailbox(props.accountId, c)
    appStore.showSuccess(t('admin.accounts.mailbox.saved'))
    emit('saved', updated)
  } catch (err: unknown) {
    error.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}

function errorMessage(err: unknown): string {
  const e = err as { message?: string; response?: { data?: { message?: string } } }
  return e?.response?.data?.message || e?.message || String(err)
}

if (props.autoPull && config.value.username && (config.value.password || canUseSavedPassword.value)) {
  void pull()
}

defineExpose({ pull })
</script>
