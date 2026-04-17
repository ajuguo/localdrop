<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

const records = ref([])
const storage = ref({
  totalBytes: 0,
  dbBytes: 0,
  imageBytes: 0,
  fileBytes: 0
})
const loading = ref(true)
const uploadingFile = ref(false)
const cleaning = ref(false)
const dragActive = ref(false)
const notice = ref('')
const errorMessage = ref('')
const previewRecord = ref(null)
const activeModule = ref(null)
const lastSyncedAt = ref(null)
const filterQuery = ref('')
const textDraft = ref('')

const fileInput = ref(null)
const imageInput = ref(null)

let pollTimer = null

const pinnedCount = computed(() => records.value.filter((item) => item.isTop).length)
const imageCount = computed(() => records.value.filter((item) => item.contentType === 'image').length)
const fileCount = computed(() => records.value.filter((item) => item.contentType === 'file').length)
const textCount = computed(() => records.value.filter((item) => item.contentType === 'text').length)
const normalizedFilterQuery = computed(() => filterQuery.value.trim().toLowerCase())
const filteredRecords = computed(() => {
  const keyword = normalizedFilterQuery.value
  if (!keyword) {
    return records.value
  }
  return records.value.filter((record) => {
    if (record.contentType === 'text') {
      return (record.contentBody || '').toLowerCase().includes(keyword)
    }
    return fileLabel(record).toLowerCase().includes(keyword)
  })
})
const imageRecords = computed(() => filteredRecords.value.filter((record) => record.contentType === 'image'))
const fileRecords = computed(() => filteredRecords.value.filter((record) => record.contentType === 'file'))
const textRecords = computed(() => filteredRecords.value.filter((record) => record.contentType === 'text'))
const moduleCards = computed(() => [
  {
    id: 'image',
    label: 'Gallery',
    title: '图片相册',
    count: imageRecords.value.length,
    detail: '平铺浏览并点击放大'
  },
  {
    id: 'file',
    label: 'Files',
    title: '文件列表',
    count: fileRecords.value.length,
    detail: '直接下载并管理文件'
  },
  {
    id: 'text',
    label: 'Texts',
    title: '文本时间流',
    count: textRecords.value.length,
    detail: '按时间查看和复制文本'
  }
])
const activeModuleMeta = computed(() => moduleCards.value.find((card) => card.id === activeModule.value) || null)
const activeModuleRecords = computed(() => {
  if (activeModule.value === 'image') return imageRecords.value
  if (activeModule.value === 'file') return fileRecords.value
  if (activeModule.value === 'text') return textRecords.value
  return []
})

const statCards = computed(() => [
  {
    label: '当前占用',
    value: formatBytes(storage.value.totalBytes),
    detail: `${formatBytes(storage.value.imageBytes)} images + ${formatBytes(storage.value.fileBytes)} files + ${formatBytes(storage.value.dbBytes)} db`
  },
  {
    label: '置顶消息',
    value: `${pinnedCount.value}`,
    detail: '在各自分组内优先显示'
  },
  {
    label: '图片数量',
    value: `${imageCount.value}`,
    detail: '相册视图浏览与预览'
  },
  {
    label: '文件数量',
    value: `${fileCount.value}`,
    detail: `${textCount.value} 条文本同步记录`
  }
])

onMounted(() => {
  void refreshAll()
  pollTimer = window.setInterval(() => {
    void refreshAll({ silent: true })
  }, 3000)
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  if (pollTimer) {
    window.clearInterval(pollTimer)
  }
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
  if (!silent) {
    loading.value = true
  }
  try {
    const [recordPayload, storagePayload] = await Promise.all([
      apiFetch('/api/records'),
      apiFetch('/api/storage')
    ])
    records.value = recordPayload.records || []
    storage.value = storagePayload.storage || storage.value
    lastSyncedAt.value = new Date()
    if (silent) {
      errorMessage.value = ''
    }
  } catch (error) {
    errorMessage.value = error.message
  } finally {
    if (!silent) {
      loading.value = false
    }
  }
}

async function uploadText(content) {
  await apiFetch('/api/records/text', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
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
  if (!file) {
    return
  }
  await uploadBinaryBlob(file, file.name || `upload-${Date.now()}`)
  notice.value = file.type.startsWith('image/') ? '图片已上传' : '文件已上传'
}

function openFileDialog() {
  fileInput.value?.click()
}

function openImageDialog() {
  imageInput.value?.click()
}

function onDragEnter() {
  dragActive.value = true
}

function onDragLeave(event) {
  if (event.currentTarget === event.target) {
    dragActive.value = false
  }
}

async function onDrop(event) {
  dragActive.value = false
  const [file] = event.dataTransfer?.files || []
  if (!file) {
    return
  }
  await uploadBinaryBlob(file, file.name || `drop-${Date.now()}`)
  notice.value = file.type.startsWith('image/') ? '图片已通过拖拽上传' : '文件已通过拖拽上传'
}

async function toggleTop(record) {
  errorMessage.value = ''
  try {
    await apiFetch(`/api/records/${record.id}/top`, {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        isTop: !record.isTop
      })
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
  if (!confirmed) {
    return
  }

  errorMessage.value = ''
  try {
    await apiFetch(`/api/records/${record.id}`, {
      method: 'DELETE'
    })
    notice.value = '记录已删除'
    await refreshAll({ silent: true })
  } catch (error) {
    errorMessage.value = error.message
  }
}

async function cleanupOldImages() {
  const confirmed = window.confirm('将删除 7 天前的图片记录及原始图片文件，但保留普通文件和所有文本记录。确定继续吗？')
  if (!confirmed) {
    return
  }

  cleaning.value = true
  errorMessage.value = ''
  try {
    const payload = await apiFetch('/api/cleanup/old-images', {
      method: 'POST'
    })
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
  } catch (error) {
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
  if (parts.length < 2) {
    return 'FILE'
  }
  return parts.at(-1).slice(0, 6).toUpperCase()
}

function clearFilter() {
  filterQuery.value = ''
}

function openModule(moduleId) {
  filterQuery.value = ''
  activeModule.value = moduleId
}

function closeModule() {
  activeModule.value = null
  filterQuery.value = ''
}

function formatBytes(bytes) {
  if (!bytes) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB']
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / 1024 ** exponent
  return `${value.toFixed(value >= 10 || exponent === 0 ? 0 : 1)} ${units[exponent]}`
}

function formatTimestamp(value) {
  if (!value) {
    return ''
  }
  return new Date(value).toLocaleString('zh-CN', {
    hour12: false,
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

</script>

<template>
  <main class="relative overflow-hidden">
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div class="absolute left-[-8rem] top-10 h-72 w-72 rounded-full bg-emerald-400/10 blur-3xl animate-drift"></div>
      <div class="absolute right-[-5rem] top-40 h-80 w-80 rounded-full bg-cyan-400/10 blur-3xl animate-drift"></div>
    </div>

    <div class="relative mx-auto flex min-h-screen w-full max-w-6xl flex-col gap-6 px-4 py-6 sm:px-6 lg:px-8">
      <header class="min-w-0 overflow-hidden rounded-[24px] border border-white/10 bg-[color:var(--panel)] px-4 py-4 backdrop-blur-xl sm:px-5">
        <div class="flex flex-wrap items-center gap-3">
          <div class="mr-auto min-w-0">
            <p class="text-xs uppercase tracking-[0.25em] text-slate-500">LocalDrop</p>
            <div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-slate-300">
              <span class="rounded-full border border-white/10 bg-white/[0.04] px-3 py-1">3 秒轮询</span>
              <span class="rounded-full border border-white/10 bg-white/[0.04] px-3 py-1">无账号</span>
              <span class="rounded-full border border-white/10 bg-white/[0.04] px-3 py-1">SQLite + 本地文件</span>
            </div>
          </div>

          <div class="grid min-w-0 flex-1 gap-2 sm:grid-cols-2 xl:grid-cols-4">
            <article
              v-for="card in statCards"
              :key="card.label"
              class="min-w-0 rounded-[18px] border border-white/10 bg-[color:var(--panel-strong)] px-3 py-3"
            >
              <div class="flex items-baseline justify-between gap-3">
                <p class="truncate text-xs uppercase tracking-[0.22em] text-slate-500">{{ card.label }}</p>
                <p class="shrink-0 text-lg font-semibold text-slate-50">{{ card.value }}</p>
              </div>
              <p class="mt-2 truncate text-xs text-slate-400">{{ card.detail }}</p>
            </article>
          </div>
        </div>
      </header>

      <section class="grid gap-6 lg:grid-cols-[1.05fr_1.4fr] lg:items-start">
        <div class="min-w-0 space-y-4">
          <article class="min-w-0 overflow-hidden rounded-[28px] border border-white/10 bg-[color:var(--panel)] p-5 backdrop-blur-xl sm:p-6">
            <div class="flex items-start justify-between gap-4">
              <div>
                <p class="text-xs uppercase tracking-[0.25em] text-slate-400">Control Deck</p>
                <h2 class="mt-2 text-2xl font-semibold text-slate-50">同步入口</h2>
                <p class="mt-2 text-sm leading-6 text-slate-400">
                  直接输入文本并写入文本分组。图片和常见文件仍然可以通过选择文件或拖拽上传。
                </p>
              </div>
              <button
                class="rounded-full border border-rose-400/20 bg-rose-400/10 px-4 py-2 text-sm font-medium text-rose-100 transition hover:bg-rose-400/20 disabled:cursor-not-allowed disabled:opacity-60"
                :disabled="cleaning"
                @click="cleanupOldImages"
              >
                {{ cleaning ? '清理中...' : '清理一周前图片' }}
              </button>
            </div>

            <div class="mt-6 grid gap-3 sm:grid-cols-2">
              <button
                class="rounded-[22px] border border-cyan-300/20 bg-cyan-300/10 px-5 py-4 text-left text-cyan-50 transition hover:bg-cyan-300/20 disabled:cursor-not-allowed disabled:opacity-60"
                :disabled="uploadingFile"
                @click="openImageDialog"
              >
                <span class="block text-xs uppercase tracking-[0.3em] text-cyan-100/70">Upload</span>
                <span class="mt-2 block text-lg font-semibold">
                  {{ uploadingFile ? '上传中...' : '选择图片文件' }}
                </span>
              </button>

              <div class="rounded-[22px] border border-emerald-400/20 bg-emerald-400/10 p-4 text-slate-50 sm:col-span-2">
                <label for="text-sync-input" class="block text-xs uppercase tracking-[0.3em] text-emerald-100/70">Text</label>
                <textarea
                  id="text-sync-input"
                  v-model="textDraft"
                  class="mt-3 min-h-28 w-full rounded-2xl border border-emerald-100/15 bg-slate-950/40 p-3 text-sm text-slate-50 outline-none placeholder:text-slate-500"
                  placeholder="输入要同步到文本分组的内容"
                ></textarea>
                <div class="mt-3 flex flex-wrap items-center justify-between gap-3">
                  <p class="text-sm text-emerald-50/70">适合手动输入短文本、链接或临时备注。</p>
                  <button
                    class="rounded-full bg-emerald-400 px-4 py-2 text-sm font-medium text-slate-950 transition hover:bg-emerald-300"
                    @click="submitTextDraft"
                  >
                    添加文本记录
                  </button>
                </div>
              </div>
            </div>

            <button
              class="mt-3 w-full rounded-[22px] border border-white/10 bg-white/[0.05] px-5 py-4 text-left text-slate-100 transition hover:bg-white/[0.08] disabled:cursor-not-allowed disabled:opacity-60"
              :disabled="uploadingFile"
              @click="openFileDialog"
            >
              <span class="block text-xs uppercase tracking-[0.3em] text-slate-400">File</span>
              <span class="mt-2 block text-lg font-semibold">
                {{ uploadingFile ? '上传中...' : '选择任意文件并保留下载入口' }}
              </span>
            </button>

            <input ref="imageInput" type="file" accept="image/*" class="hidden" @change="handleSelectedFile" />
            <input ref="fileInput" type="file" class="hidden" @change="handleSelectedFile" />

            <div
              class="mt-4 rounded-[24px] border border-dashed px-5 py-6 text-center transition"
              :class="dragActive ? 'border-emerald-300 bg-emerald-400/10' : 'border-white/15 bg-white/[0.03]'"
              @dragenter.prevent="onDragEnter"
              @dragover.prevent="onDragEnter"
              @dragleave.prevent="onDragLeave"
              @drop.prevent="onDrop"
            >
              <p class="text-base font-medium text-slate-100">拖拽文件到这里</p>
              <p class="mt-2 text-sm text-slate-400">支持直接从桌面拖入 PNG / JPG / WEBP / PDF / TXT 等文件</p>
            </div>
          </article>

          <article
            v-if="notice || errorMessage || lastSyncedAt"
            class="min-w-0 overflow-hidden rounded-[24px] border border-white/10 bg-[color:var(--panel)] p-5 backdrop-blur-xl"
          >
            <p class="text-xs uppercase tracking-[0.25em] text-slate-400">状态面板</p>
            <p v-if="notice" class="mt-3 rounded-2xl border border-emerald-400/20 bg-emerald-400/10 px-4 py-3 text-sm text-emerald-100">
              {{ notice }}
            </p>
            <p v-if="errorMessage" class="mt-3 rounded-2xl border border-rose-400/20 bg-rose-400/10 px-4 py-3 text-sm text-rose-100">
              {{ errorMessage }}
            </p>
            <p v-if="lastSyncedAt" class="mt-3 text-sm text-slate-400">
              最近刷新: {{ formatTimestamp(lastSyncedAt) }}
            </p>
          </article>
        </div>

        <section class="min-w-0 overflow-hidden rounded-[28px] border border-white/10 bg-[color:var(--panel)] p-5 backdrop-blur-xl sm:p-6">
          <div class="flex flex-wrap items-center justify-between gap-4">
            <div>
              <p class="text-xs uppercase tracking-[0.25em] text-slate-400">Content Hub</p>
              <h2 class="mt-2 text-2xl font-semibold text-slate-50">聚合展示入口</h2>
              <p class="mt-2 text-sm text-slate-400">点击任一模块，在独立窗口里浏览对应内容。</p>
            </div>
            <button
              class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm text-slate-200 transition hover:bg-white/10"
              @click="refreshAll"
            >
              立即刷新
            </button>
          </div>

          <div v-if="loading" class="mt-8 rounded-[24px] border border-white/10 bg-white/[0.03] px-5 py-12 text-center text-slate-400">
            正在加载 LocalDrop 内容...
          </div>

          <div v-else class="mt-6 grid gap-3 sm:grid-cols-3">
            <button
              v-for="card in moduleCards"
              :key="card.id"
              class="group min-w-0 rounded-[24px] border border-white/10 bg-white/[0.03] p-4 text-left transition hover:-translate-y-0.5 hover:border-white/20 hover:bg-white/[0.05]"
              @click="openModule(card.id)"
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <p class="text-xs uppercase tracking-[0.25em] text-slate-500">{{ card.label }}</p>
                  <h3 class="mt-2 text-lg font-semibold text-slate-50">{{ card.title }}</h3>
                </div>
                <span class="shrink-0 rounded-full border border-white/10 bg-white/[0.06] px-3 py-1 text-xs text-slate-200">
                  {{ card.count }}
                </span>
              </div>
              <p class="mt-3 text-sm text-slate-400">{{ card.detail }}</p>
              <p class="mt-6 text-sm text-slate-200 transition group-hover:text-white">打开模块</p>
            </button>
          </div>
        </section>
      </section>
    </div>

    <teleport to="body">
      <div
        v-if="activeModuleMeta"
        class="fixed inset-0 z-40 flex items-center justify-center bg-slate-950/85 p-4 backdrop-blur-md"
        @click.self="closeModule"
      >
        <div class="flex max-h-[90vh] w-full max-w-6xl flex-col overflow-hidden rounded-[28px] border border-white/10 bg-slate-900/95 shadow-2xl">
          <div class="border-b border-white/10 px-5 py-4">
            <div class="flex flex-wrap items-center justify-between gap-4">
              <div class="min-w-0">
                <p class="text-xs uppercase tracking-[0.25em] text-slate-500">{{ activeModuleMeta.label }}</p>
                <div class="mt-2 flex flex-wrap items-center gap-3">
                  <h3 class="text-2xl font-semibold text-slate-50">{{ activeModuleMeta.title }}</h3>
                  <span class="rounded-full border border-white/10 bg-white/[0.05] px-3 py-1 text-xs text-slate-300">
                    {{ activeModuleRecords.length }} 条结果
                  </span>
                </div>
              </div>
              <div class="flex flex-wrap items-center gap-3">
                <div class="flex min-w-0 items-center gap-2 rounded-full border border-white/10 bg-white/[0.04] px-3 py-2">
                  <input
                    v-model="filterQuery"
                    type="text"
                    class="min-w-0 flex-1 bg-transparent text-sm text-slate-100 outline-none placeholder:text-slate-500 sm:min-w-[14rem]"
                    placeholder="过滤文本或文件名"
                  />
                  <button
                    v-if="filterQuery"
                    class="text-xs text-slate-400 transition hover:text-slate-200"
                    @click="clearFilter"
                  >
                    清空
                  </button>
                </div>
                <button
                  class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm text-slate-100 transition hover:bg-white/10"
                  @click="closeModule"
                >
                  关闭
                </button>
              </div>
            </div>
          </div>

          <div class="overflow-y-auto p-5 sm:p-6">
            <div v-if="activeModule === 'image'">
              <div v-if="activeModuleRecords.length" class="grid grid-cols-2 gap-3 xl:grid-cols-4">
                <article
                  v-for="record in activeModuleRecords"
                  :key="record.id"
                  class="animate-rise overflow-hidden rounded-[24px] border border-white/10 bg-white/[0.03]"
                >
                  <button
                    class="group relative block aspect-square w-full overflow-hidden bg-slate-950/50"
                    @click="previewRecord = record"
                  >
                    <img
                      :src="imageUrl(record)"
                      :alt="`LocalDrop image ${record.id}`"
                      class="h-full w-full object-cover transition duration-500 group-hover:scale-105"
                      loading="lazy"
                    />
                    <div class="absolute inset-x-0 bottom-0 flex items-end justify-between bg-gradient-to-t from-slate-950/90 via-slate-950/30 to-transparent p-3">
                      <span class="text-xs text-slate-200">{{ formatTimestamp(record.createdAt) }}</span>
                      <span
                        v-if="record.isTop"
                        class="rounded-full bg-emerald-400 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.2em] text-slate-950"
                      >
                        pinned
                      </span>
                    </div>
                  </button>
                  <div class="flex flex-wrap items-center gap-2 p-3 text-xs text-slate-400">
                    <span>{{ formatBytes(record.fileSize) }}</span>
                    <span class="truncate">{{ fileLabel(record) }}</span>
                  </div>
                  <div class="flex flex-wrap gap-2 px-3 pb-3">
                    <button
                      class="rounded-full border border-white/10 bg-white/5 px-3 py-1.5 text-xs text-slate-100 transition hover:bg-white/10"
                      @click="toggleTop(record)"
                    >
                      {{ record.isTop ? '取消置顶' : '置顶' }}
                    </button>
                    <a
                      class="rounded-full border border-amber-300/20 bg-amber-300/10 px-3 py-1.5 text-xs text-amber-100 transition hover:bg-amber-300/20"
                      :href="downloadUrl(record)"
                    >
                      下载
                    </a>
                    <button
                      class="rounded-full border border-rose-400/20 bg-rose-400/10 px-3 py-1.5 text-xs text-rose-100 transition hover:bg-rose-400/20"
                      @click="deleteRecord(record)"
                    >
                      删除
                    </button>
                  </div>
                </article>
              </div>
              <div v-else class="rounded-[22px] border border-white/10 bg-white/[0.03] px-4 py-12 text-center text-sm text-slate-400">
                还没有匹配的图片记录。
              </div>
            </div>

            <div v-else-if="activeModule === 'file'">
              <div v-if="activeModuleRecords.length" class="grid gap-4 xl:grid-cols-2">
                <article
                  v-for="record in activeModuleRecords"
                  :key="record.id"
                  class="animate-rise rounded-[24px] border border-white/10 bg-white/[0.03] p-4 transition hover:-translate-y-0.5"
                >
                  <div class="flex items-start gap-4">
                    <div class="flex h-14 w-14 shrink-0 items-center justify-center rounded-[18px] border border-amber-300/20 bg-amber-300/10 text-sm font-semibold tracking-[0.18em] text-amber-100">
                      {{ fileExtension(record) }}
                    </div>
                    <div class="min-w-0 flex-1">
                      <div class="flex flex-wrap items-center gap-2">
                        <p class="break-all text-base font-semibold text-slate-50">{{ fileLabel(record) }}</p>
                        <span
                          v-if="record.isTop"
                          class="rounded-full bg-emerald-400 px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.2em] text-slate-950"
                        >
                          pinned
                        </span>
                      </div>
                      <p v-if="record.mimeType" class="mt-1 text-sm text-slate-400">{{ record.mimeType }}</p>
                      <div class="mt-3 flex flex-wrap items-center gap-3 text-xs text-slate-500">
                        <span>{{ formatBytes(record.fileSize) }}</span>
                        <span>{{ formatTimestamp(record.createdAt) }}</span>
                        <span v-if="record.topAt">置顶于 {{ formatTimestamp(record.topAt) }}</span>
                      </div>
                    </div>
                  </div>

                  <div class="mt-4 flex flex-wrap gap-2">
                    <button
                      class="rounded-full border border-white/10 bg-white/5 px-3 py-1.5 text-xs text-slate-100 transition hover:bg-white/10"
                      @click="toggleTop(record)"
                    >
                      {{ record.isTop ? '取消置顶' : '置顶' }}
                    </button>
                    <a
                      class="rounded-full border border-amber-300/20 bg-amber-300/10 px-3 py-1.5 text-xs text-amber-100 transition hover:bg-amber-300/20"
                      :href="downloadUrl(record)"
                    >
                      下载
                    </a>
                    <button
                      class="rounded-full border border-rose-400/20 bg-rose-400/10 px-3 py-1.5 text-xs text-rose-100 transition hover:bg-rose-400/20"
                      @click="deleteRecord(record)"
                    >
                      删除
                    </button>
                  </div>
                </article>
              </div>
              <div v-else class="rounded-[22px] border border-white/10 bg-white/[0.03] px-4 py-12 text-center text-sm text-slate-400">
                还没有匹配的文件记录。
              </div>
            </div>

            <div v-else>
              <div v-if="activeModuleRecords.length" class="space-y-4">
                <article
                  v-for="record in activeModuleRecords"
                  :key="record.id"
                  class="animate-rise overflow-hidden rounded-[24px] border p-4 transition hover:-translate-y-0.5 sm:p-5"
                  :class="record.isTop ? 'border-emerald-300/30 bg-emerald-400/10' : 'border-white/10 bg-white/[0.03]'"
                >
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="rounded-full bg-slate-200/10 px-3 py-1 text-xs uppercase tracking-[0.25em] text-slate-200">
                      text
                    </span>
                    <span
                      v-if="record.isTop"
                      class="rounded-full bg-emerald-400 px-3 py-1 text-xs font-semibold uppercase tracking-[0.25em] text-slate-950"
                    >
                      pinned
                    </span>
                    <span class="ml-auto text-xs text-slate-400">{{ formatTimestamp(record.createdAt) }}</span>
                  </div>

                  <div class="mt-4 whitespace-pre-wrap break-words text-sm leading-7 text-slate-100 sm:text-base">
                    {{ record.contentBody }}
                  </div>

                  <div class="mt-4 flex flex-wrap items-center gap-3 text-sm text-slate-400">
                    <span v-if="record.topAt">置顶于 {{ formatTimestamp(record.topAt) }}</span>
                  </div>

                  <div class="mt-4 flex flex-wrap gap-3">
                    <button
                      class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm text-slate-100 transition hover:bg-white/10"
                      @click="toggleTop(record)"
                    >
                      {{ record.isTop ? '取消置顶' : '置顶' }}
                    </button>
                    <button
                      class="rounded-full border border-cyan-300/20 bg-cyan-300/10 px-4 py-2 text-sm text-cyan-100 transition hover:bg-cyan-300/20"
                      @click="copyText(record.contentBody)"
                    >
                      复制文本
                    </button>
                    <button
                      class="rounded-full border border-rose-400/20 bg-rose-400/10 px-4 py-2 text-sm text-rose-100 transition hover:bg-rose-400/20"
                      @click="deleteRecord(record)"
                    >
                      删除
                    </button>
                  </div>
                </article>
              </div>
              <div v-else class="rounded-[22px] border border-white/10 bg-white/[0.03] px-4 py-12 text-center text-sm text-slate-400">
                还没有匹配的文本记录。
              </div>
            </div>
          </div>
        </div>
      </div>

      <div
        v-if="previewRecord"
        class="fixed inset-0 z-50 flex items-center justify-center bg-slate-950/90 p-4 backdrop-blur-md"
        @click.self="previewRecord = null"
      >
        <div class="w-full max-w-5xl overflow-hidden rounded-[28px] border border-white/10 bg-slate-900/90 shadow-2xl">
          <div class="flex items-center justify-between border-b border-white/10 px-5 py-4">
            <div>
              <p class="text-xs uppercase tracking-[0.25em] text-slate-500">Image Preview</p>
              <p class="mt-1 text-sm text-slate-300">{{ formatTimestamp(previewRecord.createdAt) }}</p>
            </div>
            <button
              class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm text-slate-100 transition hover:bg-white/10"
              @click="previewRecord = null"
            >
              关闭
            </button>
          </div>
          <img :src="imageUrl(previewRecord)" :alt="`Preview ${previewRecord.id}`" class="max-h-[80vh] w-full object-contain" />
        </div>
      </div>
    </teleport>
  </main>
</template>
