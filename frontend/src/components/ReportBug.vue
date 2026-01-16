<script lang="ts" setup>
import { BrowserOpenURL } from '@wailsjs/runtime/runtime'

const props = withDefaults(defineProps<{
  url?: string
}>(), {
  url: 'https://accounts.shinecore.com/feedback'
})

function openFeedback() {
  BrowserOpenURL(props.url)
}
</script>

<template>
  <button class="report-bug" @click="openFeedback" :title="$t('common.report_bug')">
    <span class="report-bug__text">{{ $t('common.report_bug') }}</span>
  </button>
</template>

<style scoped>
.report-bug {
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: opacity 0.2s ease;
}

.report-bug:hover {
  opacity: 0.8;
}

.report-bug:active {
  opacity: 1;
}

.report-bug__text {
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  color: #8b949f;
}
</style>
