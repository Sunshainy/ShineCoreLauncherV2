<script lang="ts" setup>
import { BrowserOpenURL } from '@wailsjs/runtime/runtime'
import siteIcon from '@/assets/images/icon.png'

function openSite() {
  BrowserOpenURL('https://be-sunshainy.ru')
}
</script>

<template>
  <button class="discord-link" @click="openSite" aria-label="Open ShineCore website">
    <img :src="siteIcon" alt="ShineCore" class="site-icon" />
  </button>
</template>

<style scoped>
.discord-link {
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  text-decoration: none;
  background-color: rgba(255, 255, 255, 0.05);
  border-radius: 4px;
  width: 36px;
  height: 36px;
}

.discord-link img {
  display: block;
}

.site-icon {
  width: 20px;
  height: 20px;
  object-fit: contain;
}
</style>
