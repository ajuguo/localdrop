<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const records = ref([])
const bookmarks = ref([])
const storage = ref({
  totalBytes: 0,
  dbBytes: 0,
  imageBytes: 0,
  fileBytes: 0,
  wallpaperBytes: 0
})
const loading = ref(true)
const uploadingFile = ref(false)
const importingBookmarks = ref(false)
const cleaning = ref(false)
const dragActive = ref(false)
const notice = ref('')
const errorMessage = ref('')
const previewRecord = ref(null)
const activeModule = ref(null)
const lastSyncedAt = ref(null)
const bookmarkSyncedAt = ref(null)
const filterQuery = ref('')
const activeTagFilter = ref('')
const textDraft = ref('')
const renameDrafts = ref({})
const tagDrafts = ref({})
const wallpaper = ref({
  imageUrl: '',
  title: '',
  copyright: '',
  copyrightLink: '',
  date: ''
})
const wallpaperState = ref('idle')
const panelOpacity = ref(88)

const fileInput = ref(null)
const imageInput = ref(null)
const bookmarkInput = ref(null)

let pollTimer = null
let wallpaperTimer = null
let panelOpacityTimer = null

const pinnedCount = computed(() => records.value.filter((item) => item.isTop).length)
const imageCount = computed(() => records.value.filter((item) => item.contentType === 'image').length)
const fileCount = computed(() => records.value.filter((item) => item.contentType === 'file').length)
const textCount = computed(() => records.value.filter((item) => item.contentType === 'text').length)
const bookmarkCount = computed(() => bookmarks.value.length)
const normalizedFilterQuery = computed(() => filterQuery.value.trim().toLowerCase())
const filteredRecords = computed(() => {
  const keyword = normalizedFilterQuery.value
  if (!keyword) return records.value
  return records.value.filter((record) => {
    if (record.contentType === 'text') {
      return (record.contentBody || '').toLowerCase().includes(keyword)
    }
    return fileLabel(record).toLowerCase().includes(keyword)
  })
})
const filteredBookmarks = computed(() => {
  const keyword = normalizedFilterQuery.value
  if (!keyword) return bookmarks.value
  return bookmarks.value.filter((bookmark) =>
    [bookmark.title, bookmark.url, bookmark.folderPath]
      .join(' ')
      .toLowerCase()
      .includes(keyword)
  )
})
const imageBaseRecords = computed(() => filteredRecords.value.filter((record) => record.contentType === 'image'))
const fileBaseRecords = computed(() => filteredRecords.value.filter((record) => record.contentType === 'file'))
const textBaseRecords = computed(() => filteredRecords.value.filter((record) => record.contentType === 'text'))
const imageRecords = computed(() => applyTagFilter(imageBaseRecords.value))
const fileRecords = computed(() => applyTagFilter(fileBaseRecords.value))
const textRecords = computed(() => applyTagFilter(textBaseRecords.value))
const moduleCards = computed(() => [
  {
    id: 'bookmark',
    label: 'BOOKMARKS',
    title: '书签图鉴',
    count: bookmarkCount.value,
    detail: '导入浏览器书签并集中检索',
    tone: 'sky'
  },
  {
    id: 'image',
    label: 'GALLERY',
    title: '像素画廊',
    count: imageCount.value,
    detail: '平铺浏览图片并放大预览',
    tone: 'mint'
  },
  {
    id: 'file',
    label: 'FILES',
    title: '文件仓',
    count: fileCount.value,
    detail: '下载、改名、打标签、置顶',
    tone: 'amber'
  },
  {
    id: 'text',
    label: 'TEXTS',
    title: '文本栈',
    count: textCount.value,
    detail: '记录短文本、链接和临时备注',
    tone: 'rose'
  }
])
const activeModuleMeta = computed(() => moduleCards.value.find((card) => card.id === activeModule.value) || null)
const activeModuleRecords = computed(() => {
  if (activeModule.value === 'bookmark') return filteredBookmarks.value
  if (activeModule.value === 'image') return imageRecords.value
  if (activeModule.value === 'file') return fileRecords.value
  if (activeModule.value === 'text') return textRecords.value
  return []
})
const activeModuleTags = computed(() => {
  if (activeModule.value === 'bookmark') return []
  let recordsForTags = []
  if (activeModule.value === 'image') recordsForTags = imageBaseRecords.value
  if (activeModule.value === 'file') recordsForTags = fileBaseRecords.value
  if (activeModule.value === 'text') recordsForTags = textBaseRecords.value
  return uniqueTags(recordsForTags)
})
const wallpaperStyle = computed(() => ({
  '--bing-wallpaper': wallpaper.value.imageUrl ? `url("${wallpaper.value.imageUrl}")` : 'none',
  '--island-panel-alpha': `${(panelOpacity.value / 100).toFixed(2)}`,
  '--island-panel-strong-alpha': `${Math.min(panelOpacity.value / 100 + 0.06, 0.98).toFixed(2)}`
}))
const wallpaperAttributionUrl = computed(() => {
  const value = wallpaper.value.copyrightLink || ''
  if (!value) return ''
  if (value.startsWith('http://') || value.startsWith('https://')) return value
  return `https://www.bing.com${value}`
})
const statCards = computed(() => [
  {
    label: 'MEMORY',
    value: formatBytes(storage.value.totalBytes),
    detail: `${formatBytes(storage.value.imageBytes)} IMG / ${formatBytes(storage.value.fileBytes)} FILE / ${formatBytes(storage.value.wallpaperBytes)} WALL / ${formatBytes(storage.value.dbBytes)} DB`
  },
  {
    label: 'PINNED',
    value: `${pinnedCount.value}`,
    detail: '置顶记录会优先显示在模块顶部'
  },
  {
    label: 'SYNC',
    value: `${imageCount.value + fileCount.value + textCount.value}`,
    detail: `${bookmarkCount.value} 条书签同步记录已归档`
  },
  {
    label: 'POLL',
    value: '03s',
    detail: '前端按 3 秒节奏轮询服务端'
  }
])

onMounted(() => {
  void fetchUISettings()
  void refreshAll()
  void fetchBingWallpaper()
  pollTimer = window.setInterval(() => {
    void refreshAll({ silent: true })
  }, 3000)
  wallpaperTimer = window.setInterval(() => {
    void fetchBingWallpaper({ silent: true })
  }, 60 * 60 * 1000)
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  if (pollTimer) window.clearInterval(pollTimer)
  if (wallpaperTimer) window.clearInterval(wallpaperTimer)
  if (panelOpacityTimer) window.clearTimeout(panelOpacityTimer)
  window.removeEventListener('keydown', onKeydown)
})

function onKeydown(event) {
  if (event.key === 'Escape') {
    if (previewRecord.value) {
      previewRecord.value = null
      return
    }
    closeModule()
  }
}

async function apiFetch(url, options = {}) {
  const response = await fetch(url, options)
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    throw new Error(payload.error || `Request failed with ${response.status}`)
  }
  return payload
}

async function refreshAll({ silent = false } = {}) {
  if (!silent) loading.value = true
  try {
    const [recordPayload, storagePayload, bookmarkPayload] = await Promise.all([
      apiFetch('/api/records'),
      apiFetch('/api/storage'),
      apiFetch('/api/bookmarks')
    ])
    records.value = recordPayload.records || []
    storage.value = storagePayload.storage || storage.value
    bookmarks.value = bookmarkPayload.bookmarks || []
    lastSyncedAt.value = new Date()
    bookmarkSyncedAt.value = bookmarkPayload.syncedAt ? new Date(bookmarkPayload.syncedAt) : null
    if (silent) errorMessage.value = ''
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    if (!silent) loading.value = false
  }
}

async function fetchBingWallpaper({ silent = false } = {}) {
  if (!silent) wallpaperState.value = 'loading'
  try {
    const payload = await apiFetch('/api/wallpaper/bing')
    if (payload.wallpaper?.imageUrl) {
      wallpaper.value = {
        imageUrl: payload.wallpaper.imageUrl,
        title: payload.wallpaper.title || 'Bing Daily Wallpaper',
        copyright: payload.wallpaper.copyright || '',
        copyrightLink: payload.wallpaper.copyrightLink || '',
        date: payload.wallpaper.date || ''
      }
      wallpaperState.value = 'ready'
    } else if (!silent) {
      wallpaperState.value = 'error'
    }
  } catch (error) {
    if (!silent) {
      wallpaperState.value = 'error'
      errorMessage.value = error.message
    }
  }
}

async function fetchUISettings() {
  try {
    const payload = await apiFetch('/api/settings/ui')
    if (typeof payload.settings?.panelOpacity === 'number') {
      panelOpacity.value = payload.settings.panelOpacity
    }
  } catch (error) {
    errorMessage.value = error.message
  }
}

function updatePanelOpacity(value) {
  const numeric = Number.parseInt(value, 10)
  if (Number.isNaN(numeric)) return
  panelOpacity.value = Math.min(100, Math.max(35, numeric))
  if (panelOpacityTimer) window.clearTimeout(panelOpacityTimer)
  panelOpacityTimer = window.setTimeout(() => {
    void saveUISettings()
  }, 180)
}

async function refreshWallpaperNow() {
  await apiFetch('/api/wallpaper/bing?refresh=1').then((payload) => {
    if (payload.wallpaper?.imageUrl) {
      wallpaper.value = {
        imageUrl: payload.wallpaper.imageUrl,
        title: payload.wallpaper.title || 'Bing Daily Wallpaper',
        copyright: payload.wallpaper.copyright || '',
        copyrightLink: payload.wallpaper.copyrightLink || '',
        date: payload.wallpaper.date || ''
      }
      wallpaperState.value = 'ready'
    }
  }).catch((error) => {
    wallpaperState.value = 'error'
    errorMessage.value = error.message
  })
}

async function saveUISettings() {
  try {
    const payload = await apiFetch('/api/settings/ui', {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ panelOpacity: panelOpacity.value })
    })
    if (typeof payload.settings?.panelOpacity === 'number') {
      panelOpacity.value = payload.settings.panelOpacity
    }
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function uploadText(content) {
  await apiFetch('/api/records/text', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ content })
  })
  await refreshAll({ silent: true })
}

async function submitTextDraft() {
  const content = textDraft.value.trim()
  if (!content) {
    errorMessage.value = '请输入要同步的文本内容'
    return
  }

  errorMessage.value = ''
  try {
    await uploadText(content)
    textDraft.value = ''
    notice.value = '文本已同步到文本分组'
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function uploadBinaryBlob(blob, filename, endpoint = '/api/records/file') {
  uploadingFile.value = true
  errorMessage.value = ''
  try {
    const formData = new FormData()
    formData.append('file', blob, filename)
    await apiFetch(endpoint, {
      method: 'POST',
      body: formData
    })
    await refreshAll({ silent: true })
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    uploadingFile.value = false
  }
}

async function handleSelectedFile(event) {
  const [file] = event.target.files || []
  event.target.value = ''
  if (!file) return
  await uploadBinaryBlob(file, file.name || `upload-${Date.now()}`)
  notice.value = file.type.startsWith('image/') ? '图片已上传' : '文件已上传'
}

function openFileDialog() {
  fileInput.value?.click()
}

function openImageDialog() {
  imageInput.value?.click()
}

function openBookmarkDialog() {
  bookmarkInput.value?.click()
}

function onDragEnter() {
  dragActive.value = true
}

function onDragLeave(event) {
  if (event.currentTarget === event.target) dragActive.value = false
}

async function onDrop(event) {
  dragActive.value = false
  const [file] = event.dataTransfer?.files || []
  if (!file) return
  await uploadBinaryBlob(file, file.name || `drop-${Date.now()}`)
  notice.value = file.type.startsWith('image/') ? '图片已通过拖拽上传' : '文件已通过拖拽上传'
}

async function handleBookmarkFile(event) {
  const [file] = event.target.files || []
  event.target.value = ''
  if (!file) return
  await uploadBookmarkFile(file)
}

async function uploadBookmarkFile(file) {
  importingBookmarks.value = true
  errorMessage.value = ''
  try {
    const formData = new FormData()
    formData.append('file', file, file.name || 'bookmarks.html')
    const payload = await apiFetch('/api/bookmarks/import', {
      method: 'POST',
      body: formData
    })
    await refreshAll({ silent: true })
    notice.value = payload.importedCount ? `已同步 ${payload.importedCount} 条浏览器书签` : '书签已同步'
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    importingBookmarks.value = false
  }
}

async function toggleTop(record) {
  errorMessage.value = ''
  try {
    await apiFetch(`/api/records/${record.id}/top`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ isTop: !record.isTop })
    })
    notice.value = record.isTop ? '已取消置顶' : '已在当前分组置顶'
    await refreshAll({ silent: true })
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function deleteRecord(record) {
  const confirmed = window.confirm(record.contentType === 'text'
    ? '确定删除这条文本记录吗？'
    : '删除后会同时移除服务器上的原始文件，确定继续吗？')
  if (!confirmed) return

  errorMessage.value = ''
  try {
    await apiFetch(`/api/records/${record.id}`, { method: 'DELETE' })
    notice.value = '记录已删除'
    await refreshAll({ silent: true })
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function cleanupOldImages() {
  const confirmed = window.confirm('将删除 7 天前的图片记录及原始图片文件，但保留普通文件和所有文本记录。确定继续吗？')
  if (!confirmed) return

  cleaning.value = true
  errorMessage.value = ''
  try {
    const payload = await apiFetch('/api/cleanup/old-images', { method: 'POST' })
    storage.value = payload.storage || storage.value
    notice.value = payload.deletedCount ? `已清理 ${payload.deletedCount} 张旧图片` : '没有可清理的旧图片'
    await refreshAll({ silent: true })
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    cleaning.value = false
  }
}

async function copyText(value) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(value)
    } else {
      fallbackCopy(value)
    }
    notice.value = '文本已复制'
  } catch {
    fallbackCopy(value)
    notice.value = '文本已复制'
  }
}

function fallbackCopy(value) {
  const textarea = document.createElement('textarea')
  textarea.value = value
  textarea.setAttribute('readonly', 'readonly')
  textarea.style.position = 'fixed'
  textarea.style.left = '-9999px'
  document.body.appendChild(textarea)
  textarea.select()
  document.execCommand('copy')
  document.body.removeChild(textarea)
}

function imageUrl(record) {
  return `/media/${record.contentBody}`
}

function downloadUrl(record) {
  return `/api/records/${record.id}/download`
}

function fileLabel(record) {
  return record.fileName || '未命名文件'
}

function fileExtension(record) {
  const label = fileLabel(record)
  const parts = label.split('.')
  if (parts.length < 2) return 'FILE'
  return parts.at(-1).slice(0, 6).toUpperCase()
}

function clearFilter() {
  filterQuery.value = ''
}

function openModule(moduleId) {
  filterQuery.value = ''
  activeTagFilter.value = ''
  activeModule.value = moduleId
}

function closeModule() {
  activeModule.value = null
  filterQuery.value = ''
  activeTagFilter.value = ''
}

async function updateRecordMeta(recordId, payload) {
  await apiFetch(`/api/records/${recordId}/meta`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  await refreshAll({ silent: true })
}

async function renameBinaryRecord(record) {
  const nextName = (renameDrafts.value[record.id] || '').trim()
  if (!nextName || nextName === fileLabel(record)) return

  errorMessage.value = ''
  try {
    await updateRecordMeta(record.id, { fileName: nextName })
    renameDrafts.value = {
      ...renameDrafts.value,
      [record.id]: nextName
    }
    notice.value = '名称已更新'
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function addTag(record) {
  const nextTag = (tagDrafts.value[record.id] || '').trim()
  if (!nextTag) return

  const tags = uniqueTagValues([...(record.tags || []), nextTag])
  errorMessage.value = ''
  try {
    await updateRecordMeta(record.id, { tags })
    tagDrafts.value = {
      ...tagDrafts.value,
      [record.id]: ''
    }
    notice.value = '标签已更新'
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function removeTag(record, tag) {
  const tags = (record.tags || []).filter((item) => item !== tag)
  errorMessage.value = ''
  try {
    await updateRecordMeta(record.id, { tags })
    notice.value = '标签已移除'
  } catch (error) {
    errorMessage.value = error.message
  }
}

function onRenameDraftFocus(record) {
  renameDrafts.value = {
    ...renameDrafts.value,
    [record.id]: fileLabel(record)
  }
}

function toggleTagFilter(tag) {
  activeTagFilter.value = activeTagFilter.value === tag ? '' : tag
}

function applyTagFilter(items) {
  if (!activeTagFilter.value) return items
  return items.filter((record) => (record.tags || []).includes(activeTagFilter.value))
}

function uniqueTags(items) {
  return uniqueTagValues(items.flatMap((record) => record.tags || []))
}

function uniqueTagValues(tags) {
  const seen = new Set()
  const result = []
  for (const rawTag of tags) {
    const tag = `${rawTag || ''}`.trim()
    if (!tag) continue
    const key = tag.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    result.push(tag)
  }
  return result
}

function formatBytes(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / 1024 ** exponent
  return `${value.toFixed(value >= 10 || exponent === 0 ? 0 : 1)} ${units[exponent]}`
}

function formatTimestamp(value) {
  if (!value) return ''
  return new Date(value).toLocaleString('zh-CN', {
    hour12: false,
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

function moduleEmptyText() {
  if (activeModule.value === 'bookmark') return '这里还没有匹配的书签。'
  if (activeModule.value === 'image') return '这里还没有匹配的图片记录。'
  if (activeModule.value === 'file') return '这里还没有匹配的文件记录。'
  return '这里还没有匹配的文本记录。'
}
</script>

<template>
  <main class="island-shell" :style="wallpaperStyle">
    <div class="island-backdrop"></div>
    <div class="island-grain"></div>

    <div class="relative z-10 mx-auto flex min-h-screen w-full max-w-7xl flex-col gap-5 px-4 py-4 sm:px-5 lg:px-6 lg:py-6">
      <header class="island-hero">
        <div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
          <div class="min-w-0 max-w-3xl">
            <div class="flex flex-wrap items-center gap-2">
              <span class="island-chip">LocalDrop</span>
              <span class="island-chip island-chip-soft">3s 轮询</span>
              <span class="island-chip island-chip-soft">SQLite + 本地文件</span>
              <span class="island-chip" :class="wallpaperState === 'ready' ? 'island-chip-mint' : 'island-chip-soft'">Bing 壁纸</span>
            </div>
            <p class="island-eyebrow mt-5">Island Transfer Hub</p>
            <h1 class="island-title mt-2">把局域网临时文件站，改造成一座轻松好用的无人岛码头。</h1>
            <p class="island-subtitle mt-4 max-w-2xl">
              文本、图片、文件和浏览器书签都在一个入口里管理。保留当前同步能力，只把界面重写成更接近
              animal-island-ui 的温暖圆润风格。
            </p>

            <div class="mt-6 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <article v-for="card in statCards" :key="card.label" class="island-stat">
                <p class="island-stat-label">{{ card.label }}</p>
                <p class="island-stat-value">{{ card.value }}</p>
                <p class="island-stat-detail">{{ card.detail }}</p>
              </article>
            </div>
          </div>

          <aside class="island-postcard lg:max-w-sm">
            <div class="flex items-center justify-between gap-3">
              <div>
                <p class="island-eyebrow">Today Backdrop</p>
                <p class="mt-1 text-sm text-[color:var(--island-text-muted)]">当前背景来源</p>
              </div>
              <button class="island-button island-button-secondary island-button-sm" @click="refreshWallpaperNow">刷新</button>
            </div>
            <div class="mt-4 island-postcard-frame">
              <div v-if="wallpaper.imageUrl" class="island-postcard-image" :style="{ backgroundImage: `url(${wallpaper.imageUrl})` }"></div>
              <div v-else class="island-postcard-empty">SYNCING...</div>
            </div>
            <p class="mt-4 text-sm font-semibold text-[color:var(--island-text)]">{{ wallpaper.title || 'Bing Daily Wallpaper' }}</p>
            <p class="mt-2 text-sm leading-6 text-[color:var(--island-text-soft)]">{{ wallpaper.copyright || '壁纸信息将在载入成功后显示。' }}</p>
            <div class="mt-4 flex flex-wrap gap-2">
              <a v-if="wallpaperAttributionUrl" class="island-button island-button-secondary island-button-sm" :href="wallpaperAttributionUrl" target="_blank" rel="noreferrer">查看来源</a>
            </div>
          </aside>
        </div>
      </header>

      <section class="grid gap-5 xl:grid-cols-[370px_minmax(0,1fr)]">
        <aside class="flex min-w-0 flex-col gap-5">
          <article class="island-panel">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div>
                <p class="island-eyebrow">Control Desk</p>
                <h2 class="island-panel-title mt-2">同步入口</h2>
                <p class="mt-3 text-sm leading-6 text-[color:var(--island-text-soft)]">
                  上传图片、文件、导入书签，或直接写入临时文本。所有内容都会进入右侧的模块仓库。
                </p>
              </div>
              <button class="island-button island-button-danger island-button-sm" :disabled="cleaning" @click="cleanupOldImages">
                {{ cleaning ? '清理中...' : '清理旧图片' }}
              </button>
            </div>

            <div class="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
              <button class="island-action island-action-mint" :disabled="uploadingFile" @click="openImageDialog">
                <span class="island-action-kicker">Image</span>
                <span class="island-action-title">{{ uploadingFile ? '上传中...' : '选择图片' }}</span>
              </button>
              <button class="island-action island-action-sky" :disabled="importingBookmarks" @click="openBookmarkDialog">
                <span class="island-action-kicker">Bookmarks</span>
                <span class="island-action-title">{{ importingBookmarks ? '同步中...' : '导入书签' }}</span>
              </button>
            </div>

            <div class="mt-4 island-field">
              <label for="text-sync-input" class="island-field-label">文本同步</label>
              <textarea
                id="text-sync-input"
                v-model="textDraft"
                class="island-textarea"
                placeholder="输入短文本、链接或临时备注"
              ></textarea>
              <div class="mt-3 flex flex-wrap items-center justify-between gap-3">
                <p class="text-sm text-[color:var(--island-text-muted)]">适合快速记事，不需要单独建文件。</p>
                <button class="island-button island-button-primary" @click="submitTextDraft">写入文本栈</button>
              </div>
            </div>

            <button class="mt-4 island-action island-action-amber w-full" :disabled="uploadingFile" @click="openFileDialog">
              <span class="island-action-kicker">Files</span>
              <span class="island-action-title">{{ uploadingFile ? '上传中...' : '上传任意文件' }}</span>
            </button>

            <input ref="imageInput" type="file" accept="image/*" class="hidden" @change="handleSelectedFile" />
            <input ref="fileInput" type="file" class="hidden" @change="handleSelectedFile" />
            <input ref="bookmarkInput" type="file" accept=".html,text/html" class="hidden" @change="handleBookmarkFile" />

            <div
              class="mt-4 island-dropzone"
              :class="dragActive ? 'island-dropzone-active' : ''"
              @dragenter.prevent="onDragEnter"
              @dragover.prevent="onDragEnter"
              @dragleave.prevent="onDragLeave"
              @drop.prevent="onDrop"
            >
              <p class="island-panel-title text-base">拖拽文件到这里</p>
              <p class="mt-2 text-sm leading-6 text-[color:var(--island-text-muted)]">支持 PNG、JPG、WEBP、PDF、TXT 等常见文件格式。</p>
            </div>
          </article>

          <article class="island-panel">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div>
                <p class="island-eyebrow">Window Mood</p>
                <h2 class="island-panel-title mt-2">界面透明度</h2>
              </div>
              <span class="island-badge">{{ panelOpacity }}%</span>
            </div>
            <p class="mt-3 text-sm leading-6 text-[color:var(--island-text-soft)]">调节主面板透明度，范围 35% 到 100%。</p>
            <input
              :value="panelOpacity"
              type="range"
              min="35"
              max="100"
              step="1"
              class="mt-4 island-slider"
              @input="updatePanelOpacity($event.target.value)"
            />
          </article>

          <article v-if="notice || errorMessage || lastSyncedAt" class="island-panel">
            <p class="island-eyebrow">Status Log</p>
            <div v-if="notice" class="island-message island-message-success mt-4">{{ notice }}</div>
            <div v-if="errorMessage" class="island-message island-message-danger mt-4">{{ errorMessage }}</div>
            <p v-if="lastSyncedAt" class="mt-4 text-sm text-[color:var(--island-text-soft)]">最近刷新：{{ formatTimestamp(lastSyncedAt) }}</p>
            <p v-if="bookmarkSyncedAt" class="mt-2 text-sm text-[color:var(--island-text-soft)]">书签同步：{{ formatTimestamp(bookmarkSyncedAt) }}</p>
          </article>
        </aside>

        <section class="island-panel min-w-0">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p class="island-eyebrow">Module Dock</p>
              <h2 class="island-panel-title mt-2">内容模块</h2>
              <p class="mt-3 text-sm leading-6 text-[color:var(--island-text-soft)]">每个模块都支持筛选、标签管理、置顶、删除和下载操作。</p>
            </div>
            <button class="island-button island-button-secondary" @click="refreshAll">立即刷新</button>
          </div>

          <div v-if="loading" class="island-empty mt-6">正在读取内容仓库...</div>

          <div v-else class="mt-6 grid gap-4 md:grid-cols-2 2xl:grid-cols-4">
            <button v-for="card in moduleCards" :key="card.id" class="island-module" :data-tone="card.tone" @click="openModule(card.id)">
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <p class="island-module-label">{{ card.label }}</p>
                  <h3 class="island-module-title">{{ card.title }}</h3>
                </div>
                <span class="island-badge">{{ card.count }}</span>
              </div>
              <p class="mt-4 text-sm leading-6 text-[color:var(--island-text-soft)]">{{ card.detail }}</p>
              <div class="mt-5 island-module-footer">
                <span>打开模块</span>
                <span>›</span>
              </div>
            </button>
          </div>
        </section>
      </section>
    </div>

    <teleport to="body">
      <div v-if="activeModuleMeta" class="island-modal-backdrop" @click.self="closeModule">
        <div class="island-modal">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div>
              <p class="island-eyebrow">{{ activeModuleMeta.label }}</p>
              <div class="mt-2 flex flex-wrap items-center gap-2">
                <h3 class="island-panel-title">{{ activeModuleMeta.title }}</h3>
                <span class="island-badge">{{ activeModuleRecords.length }}</span>
              </div>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <div class="island-search">
                <input v-model="filterQuery" type="text" class="island-input" placeholder="过滤标题、链接或文件名" />
              </div>
              <button v-if="filterQuery" class="island-button island-button-secondary island-button-sm" @click="clearFilter">清空</button>
              <button class="island-button island-button-secondary island-button-sm" @click="closeModule">关闭</button>
            </div>
          </div>

          <div v-if="activeModuleTags.length" class="mt-5 flex flex-wrap gap-2">
            <button class="island-tag" :class="!activeTagFilter ? 'island-tag-active' : ''" @click="activeTagFilter = ''">全部</button>
            <button v-for="tag in activeModuleTags" :key="tag" class="island-tag" :class="activeTagFilter === tag ? 'island-tag-active' : ''" @click="toggleTagFilter(tag)">#{{ tag }}</button>
          </div>

          <div class="mt-6">
            <div v-if="activeModule === 'bookmark'">
              <div v-if="activeModuleRecords.length" class="grid gap-4 xl:grid-cols-2">
                <article v-for="bookmark in activeModuleRecords" :key="`${bookmark.id}-${bookmark.url}`" class="island-record">
                  <p class="island-record-title break-all">{{ bookmark.title || '未命名书签' }}</p>
                  <p v-if="bookmark.folderPath" class="mt-2 text-sm text-[color:var(--island-text-muted)]">{{ bookmark.folderPath }}</p>
                  <a class="mt-3 block break-all text-sm font-semibold text-[color:var(--island-primary-strong)] underline decoration-dotted underline-offset-4" :href="bookmark.url" target="_blank" rel="noreferrer">{{ bookmark.url }}</a>
                </article>
              </div>
              <div v-else class="island-empty">{{ moduleEmptyText() }}</div>
            </div>

            <div v-else-if="activeModule === 'image'">
              <div v-if="activeModuleRecords.length" class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
                <article v-for="record in activeModuleRecords" :key="record.id" class="island-record island-record-media">
                  <button class="island-image-card" @click="previewRecord = record">
                    <img :src="imageUrl(record)" :alt="`LocalDrop image ${record.id}`" class="island-thumb" loading="lazy" />
                    <span v-if="record.isTop" class="island-corner-pill">TOP</span>
                  </button>
                  <div class="p-4">
                    <div class="flex flex-wrap items-center gap-2 text-sm text-[color:var(--island-text-muted)]">
                      <span>{{ formatBytes(record.fileSize) }}</span>
                      <span>{{ formatTimestamp(record.createdAt) }}</span>
                    </div>
                    <p class="mt-3 island-record-title break-all">{{ fileLabel(record) }}</p>
                    <div class="mt-3 flex gap-2">
                      <input v-model="renameDrafts[record.id]" type="text" class="island-input flex-1" :placeholder="fileLabel(record)" @focus="onRenameDraftFocus(record)" @keyup.enter="renameBinaryRecord(record)" />
                      <button class="island-button island-button-secondary island-button-sm" @click="renameBinaryRecord(record)">改名</button>
                    </div>
                    <div class="mt-3 flex flex-wrap gap-2">
                      <button v-for="tag in record.tags || []" :key="tag" class="island-tag island-tag-soft" @click="removeTag(record, tag)">#{{ tag }} ×</button>
                    </div>
                    <div class="mt-3 flex gap-2">
                      <input v-model="tagDrafts[record.id]" type="text" class="island-input flex-1" placeholder="添加标签" @keyup.enter="addTag(record)" />
                      <button class="island-button island-button-primary island-button-sm" @click="addTag(record)">添加</button>
                    </div>
                    <div class="mt-3 flex flex-wrap gap-2">
                      <button class="island-button island-button-secondary island-button-sm" @click="toggleTop(record)">{{ record.isTop ? '取消置顶' : '置顶' }}</button>
                      <a class="island-button island-button-amber island-button-sm" :href="downloadUrl(record)">下载</a>
                      <button class="island-button island-button-danger island-button-sm" @click="deleteRecord(record)">删除</button>
                    </div>
                  </div>
                </article>
              </div>
              <div v-else class="island-empty">{{ moduleEmptyText() }}</div>
            </div>

            <div v-else-if="activeModule === 'file'">
              <div v-if="activeModuleRecords.length" class="grid gap-4 xl:grid-cols-2">
                <article v-for="record in activeModuleRecords" :key="record.id" class="island-record">
                  <div class="flex items-start gap-3">
                    <div class="island-file-ext">{{ fileExtension(record) }}</div>
                    <div class="min-w-0 flex-1">
                      <div class="flex flex-wrap items-center gap-2">
                        <p class="island-record-title break-all">{{ fileLabel(record) }}</p>
                        <span v-if="record.isTop" class="island-corner-inline">TOP</span>
                      </div>
                      <p v-if="record.mimeType" class="mt-2 break-all text-sm text-[color:var(--island-text-muted)]">{{ record.mimeType }}</p>
                      <div class="mt-3 flex flex-wrap items-center gap-2 text-sm text-[color:var(--island-text-muted)]">
                        <span>{{ formatBytes(record.fileSize) }}</span>
                        <span>{{ formatTimestamp(record.createdAt) }}</span>
                      </div>
                    </div>
                  </div>
                  <div class="mt-3 flex gap-2">
                    <input v-model="renameDrafts[record.id]" type="text" class="island-input flex-1" :placeholder="fileLabel(record)" @focus="onRenameDraftFocus(record)" @keyup.enter="renameBinaryRecord(record)" />
                    <button class="island-button island-button-secondary island-button-sm" @click="renameBinaryRecord(record)">改名</button>
                  </div>
                  <div class="mt-3 flex flex-wrap gap-2">
                    <button v-for="tag in record.tags || []" :key="tag" class="island-tag island-tag-soft" @click="removeTag(record, tag)">#{{ tag }} ×</button>
                  </div>
                  <div class="mt-3 flex gap-2">
                    <input v-model="tagDrafts[record.id]" type="text" class="island-input flex-1" placeholder="添加标签" @keyup.enter="addTag(record)" />
                    <button class="island-button island-button-primary island-button-sm" @click="addTag(record)">添加</button>
                  </div>
                  <div class="mt-3 flex flex-wrap gap-2">
                    <button class="island-button island-button-secondary island-button-sm" @click="toggleTop(record)">{{ record.isTop ? '取消置顶' : '置顶' }}</button>
                    <a class="island-button island-button-amber island-button-sm" :href="downloadUrl(record)">下载</a>
                    <button class="island-button island-button-danger island-button-sm" @click="deleteRecord(record)">删除</button>
                  </div>
                </article>
              </div>
              <div v-else class="island-empty">{{ moduleEmptyText() }}</div>
            </div>

            <div v-else>
              <div v-if="activeModuleRecords.length" class="grid gap-4">
                <article v-for="record in activeModuleRecords" :key="record.id" class="island-record">
                  <div class="flex flex-wrap items-center gap-2 text-sm text-[color:var(--island-text-muted)]">
                    <span>{{ formatTimestamp(record.createdAt) }}</span>
                    <span v-if="record.isTop" class="island-corner-inline">TOP</span>
                  </div>
                  <pre class="island-pre">{{ record.contentBody }}</pre>
                  <div class="mt-3 flex flex-wrap gap-2">
                    <button v-for="tag in record.tags || []" :key="tag" class="island-tag island-tag-soft" @click="removeTag(record, tag)">#{{ tag }} ×</button>
                  </div>
                  <div class="mt-3 flex gap-2">
                    <input v-model="tagDrafts[record.id]" type="text" class="island-input flex-1" placeholder="添加标签" @keyup.enter="addTag(record)" />
                    <button class="island-button island-button-primary island-button-sm" @click="addTag(record)">添加</button>
                  </div>
                  <div class="mt-3 flex flex-wrap gap-2">
                    <button class="island-button island-button-primary island-button-sm" @click="copyText(record.contentBody)">复制</button>
                    <button class="island-button island-button-secondary island-button-sm" @click="toggleTop(record)">{{ record.isTop ? '取消置顶' : '置顶' }}</button>
                    <button class="island-button island-button-danger island-button-sm" @click="deleteRecord(record)">删除</button>
                  </div>
                </article>
              </div>
              <div v-else class="island-empty">{{ moduleEmptyText() }}</div>
            </div>
          </div>
        </div>
      </div>
    </teleport>

    <teleport to="body">
      <div v-if="previewRecord" class="island-modal-backdrop" @click.self="previewRecord = null">
        <div class="island-preview-modal">
          <img :src="imageUrl(previewRecord)" :alt="fileLabel(previewRecord)" class="max-h-[78vh] w-full rounded-[26px] object-contain bg-[#f4eddd]" />
          <div class="mt-4 flex flex-wrap items-center justify-between gap-3">
            <div>
              <p class="island-record-title break-all">{{ fileLabel(previewRecord) }}</p>
              <p class="mt-2 text-sm text-[color:var(--island-text-muted)]">{{ formatTimestamp(previewRecord.createdAt) }} / {{ formatBytes(previewRecord.fileSize) }}</p>
            </div>
            <div class="flex flex-wrap gap-2">
              <a class="island-button island-button-amber island-button-sm" :href="downloadUrl(previewRecord)">下载</a>
              <button class="island-button island-button-secondary island-button-sm" @click="previewRecord = null">关闭</button>
            </div>
          </div>
        </div>
      </div>
    </teleport>
  </main>
</template>
