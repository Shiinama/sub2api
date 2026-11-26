<template>
  <div data-testid="mailbox-message-list">
    <div
      v-if="inbox.latest_code"
      class="mb-2 flex items-center gap-3 rounded-lg border border-green-300 bg-green-50 px-3 py-2 dark:border-green-700 dark:bg-green-900/30"
    >
      <span class="text-sm text-green-800 dark:text-green-300">{{ t('admin.accounts.mailbox.latestCode') }}</span>
      <span class="font-mono text-xl font-bold tracking-widest text-green-700 dark:text-green-300">
        {{ inbox.latest_code }}
      </span>
      <button type="button" class="btn btn-secondary btn-sm ml-auto" @click="copy(inbox.latest_code)">
        <Icon name="copy" size="sm" class="mr-1" />
        {{ t('admin.accounts.mailbox.copy') }}
      </button>
    </div>

    <p v-if="inbox.messages.length === 0" class="py-3 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.mailbox.empty', { minutes: sinceMinutes }) }}
    </p>

    <ul
      v-else
      class="max-h-72 divide-y divide-gray-100 overflow-y-auto rounded-lg border border-gray-200 bg-white dark:divide-gray-700 dark:border-gray-700 dark:bg-gray-800"
    >
      <li v-for="m in inbox.messages" :key="m.uid" class="px-3 py-2">
        <div class="flex items-center gap-2">
          <span class="min-w-0 flex-1 truncate text-sm font-medium text-gray-900 dark:text-gray-100">
            {{ m.subject || '(no subject)' }}
          </span>
          <button
            v-if="m.code"
            type="button"
            class="shrink-0 rounded bg-green-100 px-2 py-0.5 font-mono text-sm font-semibold text-green-700 hover:bg-green-200 dark:bg-green-900/40 dark:text-green-300"
            :title="t('admin.accounts.mailbox.copy')"
            @click="copy(m.code)"
          >
            {{ m.code }}
          </button>
          <span class="shrink-0 text-xs text-gray-400">{{ formatTime(m.received_at) }}</span>
        </div>
        <div class="truncate text-xs text-gray-500 dark:text-gray-400">{{ m.from }}</div>
        <div v-if="m.preview" class="mt-0.5 line-clamp-2 text-xs text-gray-400 dark:text-gray-500">
          {{ m.preview }}
        </div>
      </li>
    </ul>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'
import type { MailboxInboxResult } from '@/api/admin/accounts'

defineProps<{ inbox: MailboxInboxResult; sinceMinutes: number }>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

function copy(code: string) {
  copyToClipboard(code, t('admin.accounts.mailbox.copied'))
}

function formatTime(value?: string) {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  const sameDay = d.toDateString() === new Date().toDateString()
  return sameDay ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : d.toLocaleString()
}
</script>
