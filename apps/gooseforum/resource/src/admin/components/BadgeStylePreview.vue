<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ImageOff } from '@lucide/vue'
import { useI18n } from 'vue-i18n'
import type { AdminBadge } from '@/admin/types'
import UserAvatar from '@/site/components/UserAvatar.vue'
import { badgeClass, badgeIconURL } from '@/site/utils/badge-style'

const props = defineProps<{ badge: AdminBadge }>()
const { t } = useI18n()
const failed = ref(false)
const url = computed(() => badgeIconURL(props.badge))
watch(url, () => { failed.value = false })
const previewBadge = computed(() => ({ ...props.badge, iconType: props.badge.iconType || 'asset', iconKey: props.badge.iconKey || '', source: '', reason: '', grantedAt: '' }))
</script>

<template>
  <div class="space-y-3">
    <div class="grid grid-cols-2 gap-3">
      <section v-for="theme in ['light', 'dark']" :key="theme" class="min-w-0 rounded-lg border p-3" :class="theme === 'light' ? 'border-slate-200 bg-white text-slate-900' : 'border-slate-700 bg-slate-900 text-slate-100'">
        <p class="text-xs" :class="theme === 'light' ? 'text-slate-500' : 'text-slate-400'">{{ t(`decoration.configAdmin.${theme}Preview`) }}</p>
        <div class="flex flex-col items-center gap-5 py-6">
          <UserAvatar src="/static/pic/default-avatar.webp" :alt="t('decoration.configAdmin.previewMember')" :badge="failed ? null : previewBadge" class="size-14 rounded-full" img-class="rounded-full" />
          <div class="flex min-w-0 max-w-full items-center gap-2 rounded-full px-3 py-1.5 text-sm ring-1 ring-inset" :class="badgeClass(badge.color, badge.level)">
            <ImageOff v-if="failed" class="size-5 shrink-0" aria-hidden="true" />
            <img v-else :src="url" alt="" class="size-5 shrink-0 object-contain" @error="failed = true" />
            <span class="min-w-0 break-words font-medium">{{ badge.name || t('badgeAdmin.previewName') }}</span>
          </div>
        </div>
      </section>
    </div>
    <p v-if="failed" role="status" class="text-sm leading-relaxed text-destructive">{{ t('badgeAdmin.previewUnavailable') }}</p>
    <p v-else class="text-sm leading-relaxed text-muted-foreground">{{ t('badgeAdmin.previewHint') }}</p>
  </div>
</template>
