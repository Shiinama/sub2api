import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { grok: { getOAuthCapabilities: vi.fn().mockResolvedValue({}) } },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import OAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'

describe('OAuthAuthorizationFlow state handling', () => {
  it('drops a state parsed from an old callback URL once a new auth session is generated', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'gemini',
        authUrl: 'https://accounts.google.com/o/oauth2/v2/auth?state=old-state',
        sessionId: 'session-1',
        showManualOption: true,
        initialInputMethod: 'manual',
      },
      global: { stubs: { Icon: true }, mocks: { $t: (key: string) => key } },
    })

    await wrapper.find('textarea').setValue('https://codeassist.google.com/authcode?code=abc123&state=old-state')
    await nextTick()
    const vm = wrapper.vm as unknown as { oauthState: string; authCode: string }
    expect(vm.oauthState).toBe('old-state')

    await wrapper.setProps({
      sessionId: 'session-2',
      authUrl: 'https://accounts.google.com/o/oauth2/v2/auth?state=new-state',
    })
    expect(vm.oauthState).toBe('')
  })
})
