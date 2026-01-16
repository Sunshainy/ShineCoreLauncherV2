<script lang="ts" setup>
import shinecoreLogo from '@/assets/images/shinecore.png'
</script>

<template>
  <div class="logo">
    <img class="logo__image" :src="shinecoreLogo" alt="ShineCore logo" draggable="false" />
  </div>
</template>

<style scoped>
.logo {
  display: flex;
  justify-content: center;
  align-items: center;
}

.logo__image {
  height: auto;
  max-height: 160px;
}
</style>
