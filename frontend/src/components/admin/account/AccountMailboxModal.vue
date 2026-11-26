<template>
  <BaseDialog
    :show="show"
    :title="account ? `${t('admin.accounts.mailbox.modalTitle')} - ${account.name}` : t('admin.accounts.mailbox.modalTitle')"
    width="normal"
    @close="emit('close')"
  >
    <AccountMailboxPanel
      v-if="show && account"
      :key="account.id"
      v-model="config"
      :account-id="account.id"
      :has-saved-password="hasSavedPassword"
      :auto-pull="!!savedConfig && hasSavedPassword"
      @saved="(a: Account) => emit('saved', a)"
    />
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import AccountMailboxPanel from '@/components/account/AccountMailboxPanel.vue'
import {
  accountHasSavedMailboxPassword,
  defaultMailboxConfig,
  mailboxFromAccount
} from '@/components/account/mailbox'
import type { Account } from '@/types'

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ close: []; saved: [account: Account] }>()
const { t } = useI18n()

const savedConfig = computed(() => mailboxFromAccount(props.account))
const hasSavedPassword = computed(() => accountHasSavedMailboxPassword(props.account))
const config = ref(defaultMailboxConfig())

watch(
  () => [props.show, props.account?.id] as const,
  ([show]) => {
    if (show) config.value = savedConfig.value ?? defaultMailboxConfig()
  },
  { immediate: true }
)
</script>
