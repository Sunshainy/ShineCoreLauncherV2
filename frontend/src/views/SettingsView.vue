<script lang="ts" setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/appStore'
import { useNotificationStore } from '@/stores/notificationStore'
import PanelView from '@/components/PanelView.vue'
import HyButton from '@/components/HyButton.vue'
import LauncherVersion from '@/components/LauncherVersion.vue'
import { OpenGameDirectory, GetMemorySettings, SetMemoryMB, GetInstallDir, SelectInstallDir, GetConsoleEnabled, SetConsoleEnabled, OpenConsoleWindow } from '@wailsjs/go/app/App'

const router = useRouter()
const appStore = useAppStore()
const notificationStore = useNotificationStore()
const { t } = useI18n()

const isCheckingUpdates = ref(false)
const memoryMB = ref(4096)
const memoryMinMB = ref(512)
const memoryMaxMB = ref(4096)
const installDir = ref('')
const consoleEnabled = ref(false)

const openInIcon = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABEAAAARCAYAAAA7bUf6AAAACXBIWXMAAAsTAAALEwEAmpwYAAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAC8SURBVHgBrZK9EQIhEIX5Cwgp4SJmCCnBCmzFDjxLsAM7sQTMSC3hKgB3Ax08gT3v7iXsDPDxeLs8xjiybz2dczcscE8IcWaERG8TYGNK6cIIqfJCCwSOWM9R10kJyjlfN0HAycA5P66GIAC+codyWAWpATDoedjqX8C7AWXYTSdSSgOLqQFQZfubEGvtA8I8QDnNAT8gnMrK1H4UQjCMkKIOeO8n6gES0pPW+oTromGjtAuE90Jdql2cvAClzFdGDZMFsAAAAABJRU5ErkJggg=='
const editIcon = 'data:image/svg+xml;utf8,<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="%23d2d9e2" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>'

const checkingDisabled = computed(() => {
  return isCheckingUpdates.value || appStore.updateRunning
})

const canPerformActions = computed(() => {
  return !appStore.updateRunning
})

async function openDirectory() {
  try {
    await OpenGameDirectory()
  } catch (error) {
    console.error('Failed to open directory:', error)
    notificationStore.showError(t('settings.failed_to_open_directory'))
  }
}

async function loadInstallDir() {
  try {
    const dir = await GetInstallDir()
    if (dir) {
      installDir.value = dir
    }
  } catch (error) {
    console.error('Failed to load install dir:', error)
  }
}

async function selectInstallDir() {
  try {
    const dir = await SelectInstallDir()
    if (dir) {
      installDir.value = dir
    }
  } catch (error) {
    console.error('Failed to select install dir:', error)
    notificationStore.showError(t('settings.failed_to_select_directory'))
  }
}

async function loadMemorySettings() {
  try {
    const settings = await GetMemorySettings()
    if (settings) {
      memoryMB.value = settings.currentMB || 4096
      memoryMinMB.value = settings.minMB || 512
      memoryMaxMB.value = settings.maxMB || 4096
    }
  } catch (error) {
    console.error('Failed to load memory settings:', error)
  }
}

async function loadConsoleSetting() {
  try {
    consoleEnabled.value = await GetConsoleEnabled()
  } catch (error) {
    console.error('Failed to load console setting:', error)
  }
}

async function saveConsoleSetting() {
  try {
    await SetConsoleEnabled(consoleEnabled.value)
  } catch (error) {
    console.error('Failed to save console setting:', error)
    notificationStore.showError(t('settings.failed_to_save_console'))
  }
}

async function openConsoleWindow() {
  try {
    await OpenConsoleWindow()
  } catch (error) {
    console.error('Failed to open console window:', error)
    notificationStore.showError(t('settings.failed_to_open_console'))
  }
}

async function saveMemorySettings() {
  try {
    await SetMemoryMB(Number(memoryMB.value))
  } catch (error) {
    console.error('Failed to save memory settings:', error)
    notificationStore.showError(t('settings.failed_to_save_memory'))
  }
}

async function checkForUpdates() {
  isCheckingUpdates.value = true
  notificationStore.showInfo(t('settings.checking_for_updates'))

  try {
    const hasLauncherUpdate = await appStore.checkForFreestandingLauncherUpdate()
    if (hasLauncherUpdate) {
      notificationStore.showSuccess(t('settings.new_updates_available'))
      router.push({ name: 'launcher-update' })
      return
    }

    await appStore.checkForUpdates(true)

    if (appStore.updateInfo) {
      notificationStore.showSuccess(t('settings.new_updates_available'))
    } else {
      notificationStore.showInfo(t('settings.no_updates_available'))
    }
  } catch (error) {
    router.push({ name: 'error', query: { error: String(error) } })
    notificationStore.showError(t('settings.failed_to_check_for_updates'))
  } finally {
    isCheckingUpdates.value = false
  }
}

function close() {
  router.back()
}

function openUninstall() {
  router.push({ name: 'uninstall' })
}

onMounted(() => {
  loadInstallDir()
  loadMemorySettings()
  loadConsoleSetting()
})

</script>

<template>
  <PanelView
    :title="$t('settings.title')"
    :show-close-button="true"
    :show-report-bug="false"
    :esc-handler="close"
    @close="close"
  >
    <div class="settings__section">
      <h2 class="settings__label">{{ $t('settings.directory') }}</h2>
      <div class="settings__directory-row">
        <div class="settings__directory-path">{{ installDir }}</div>
        <div class="settings__directory-actions">
          <HyButton small type="tertiary" class="settings__directory-button" @click="openDirectory">
            <span>{{ $t('settings.open_directory') }}</span>
            <img :src="openInIcon" :alt="$t('settings.open')" class="settings__directory-icon" draggable="false" />
          </HyButton>
          <HyButton small type="tertiary" class="settings__directory-edit" @click="selectInstallDir">
            <img :src="editIcon" :alt="$t('settings.edit_directory')" class="settings__directory-icon" draggable="false" />
          </HyButton>
        </div>
      </div>
    </div>

    <div class="settings__section">
      <h2 class="settings__label">{{ $t('settings.launcher_version') }}</h2>
      <LauncherVersion class="settings__version" />
    </div>

    <div class="settings__section">
      <h2 class="settings__label">{{ $t('settings.memory') }}</h2>
      <div class="settings__memory">
        <input
          v-model.number="memoryMB"
          class="settings__memory-slider"
          type="range"
          :min="memoryMinMB"
          :max="memoryMaxMB"
          step="256"
          @change="saveMemorySettings"
        />
        <div class="settings__memory-values">
          <span>{{ memoryMB }} MB</span>
          <span>Max {{ memoryMaxMB }} MB</span>
        </div>
      </div>
    </div>

    <div class="settings__section">
      <h2 class="settings__label">{{ $t('settings.console') }}</h2>
      <div class="settings__console-row">
        <label class="settings__console">
          <input type="checkbox" v-model="consoleEnabled" @change="saveConsoleSetting" />
          <span>{{ $t('settings.open_console_on_launch') }}</span>
        </label>
        <HyButton small type="tertiary" class="settings__console-button" @click="openConsoleWindow">
          {{ $t('settings.open_console_now') }}
        </HyButton>
      </div>
    </div>

    <div class="settings__actions">
      <HyButton
        class="settings__action-button"
        type="tertiary"
        @click="checkForUpdates"
        :disabled="checkingDisabled"
      >
        {{ $t('settings.check_for_updates') }}
      </HyButton>
      <HyButton
        class="settings__action-button"
        type="tertiary"
        @click="openUninstall"
        :disabled="!canPerformActions"
      >
        {{ $t('settings.uninstall') }}
      </HyButton>
    </div>
  </PanelView>
</template>

<style scoped>
.settings__section {
  margin-bottom: 20px;
}

.settings__label {
  font-size: 14px;
  font-weight: 600;
  color: #8b949f;
  margin: 0 0 8px;
  text-transform: uppercase;
}

.settings__version {
  font-weight: 400;
  color: #d2d9e2;
}

.settings__directory-button {
  display: flex;
  align-items: center;
  gap: 8px;
}

.settings__directory-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.settings__directory-path {
  flex: 1;
  color: #d2d9e2;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.settings__directory-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.settings__directory-edit {
  padding: 0 10px;
}

.settings__directory-icon {
  height: 14px;
}

.settings__actions {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 24px;
}

.settings__memory {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.settings__memory-slider {
  width: 100%;
}

.settings__memory-values {
  display: flex;
  justify-content: space-between;
  color: #d2d9e2;
  font-size: 13px;
}

.settings__console {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #d2d9e2;
  font-size: 13px;
}

.settings__console-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.settings__console-button {
  padding: 0 10px;
}

.settings__action-button {
  width: 100%;
}
</style>
