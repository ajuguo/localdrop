<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

const records = ref([])
const storage = ref({
  totalBytes: 0,
  dbBytes: 0,
  imageBytes: 0,
  fileBytes: 0
})
const loading = ref(true)
const syncingClipboard = ref(false)
const uploadingFile = ref(false)
const cleaning = ref(false)
const dragActive = ref(false)
const notice = ref('')
const errorMessage = ref('')
const pasteFallbackVisible = ref(false)
const previewRecord = ref(null)
const lastSyncedAt = ref(null)
const filterQuery = ref('')

const fileInput = ref(null)
const imageInput = ref(null)
const pasteTarget = ref(null)

let pollTimer = null

const pinnedCount = computed(() => records.value.filter((item) => item.isTop).length)
const imageCount = computed(() => records.value.filter((item) => item.contentType === 'image').length)
const fileCount = computed(() => records.value.filter((item) => item.contentType === 'file').length)
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

const statCards = computed(() => [
  {
    label: '当前占用',
    value: formatBytes(storage.value.totalBytes),
    detail: `${formatBytes(storage.value.imageBytes)} images + ${formatBytes(storage.value.fileBytes)} files + ${formatBytes(storage.value.dbBytes)} db`
  },
  {
    label: '置顶消息',
    value: `${pinnedCount.value}`,
    detail: '会固定显示在信息流最上方'
  },
  {
    label: '图片数量',
    value: `${imageCount.value}`,
    detail: '支持剪贴板、拖拽和文件选择'
  },
  {
    label: '文件数量',
    value: `${fileCount.value}`,
    detail: '支持上传后直接下载到本地'
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
    previewRecord.value = null
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

async function syncClipboard() {
  syncingClipboard.value = true
  errorMessage.value = ''

  try {
    if (navigator.clipboard?.read) {
      const clipboardItems = await navigator.clipboard.read()
      for (const item of clipboardItems) {
        const imageType = item.types.find((type) => type.startsWith('image/'))
        if (imageType) {
          const blob = await item.getType(imageType)
          await uploadBinaryBlob(blob, `clipboard${extensionFromMime(imageType)}`, '/api/records/file')
          pasteFallbackVisible.value = false
          notice.value = '已从剪贴板同步图片'
          return
        }
      }
    }

    if (navigator.clipboard?.readText) {
      const text = (await navigator.clipboard.readText()).trim()
      if (text) {
        await uploadText(text)
        pasteFallbackVisible.value = false
        notice.value = '已从剪贴板同步文本'
        return
      }
    }

    throw new Error('剪贴板里没有可同步的文本或图片')
  } catch (error) {
    notice.value = '剪贴板接口不可用，请在下方输入框执行系统粘贴'
    pasteFallbackVisible.value = true
    await nextTick()
    pasteTarget.value?.focus()
  } finally {
    syncingClipboard.value = false
  }
}

async function handlePaste(event) {
  const clipboardData = event.clipboardData
  if (!clipboardData) {
    return
  }

  const imageItem = Array.from(clipboardData.items || []).find((item) => item.type.startsWith('image/'))
  if (imageItem) {
    event.preventDefault()
    const file = imageItem.getAsFile()
    if (!file) {
      return
    }
    await uploadBinaryBlob(file, `paste${extensionFromMime(file.type)}`, '/api/records/file')
    pasteFallbackVisible.value = false
    notice.value = '已通过粘贴同步图片'
    return
  }

  const text = clipboardData.getData('text').trim()
  if (text) {
    event.preventDefault()
    await uploadText(text)
    pasteFallbackVisible.value = false
    notice.value = '已通过粘贴同步文本'
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
    notice.value = record.isTop ? '已取消置顶' : '已置顶到信息流顶部'
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

function isPreviewableImage(record) {
  return record.contentType === 'image'
}

function typeLabel(record) {
  if (record.contentType === 'image') return 'image'
  if (record.contentType === 'file') return 'file'
  return 'text'
}

function fileLabel(record) {
  return record.fileName || '未命名文件'
}

function clearFilter() {
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

function extensionFromMime(type) {
  if (type === 'image/jpeg') return '.jpg'
  if (type === 'image/png') return '.png'
  if (type === 'image/gif') return '.gif'
  if (type === 'image/webp') return '.webp'
  if (type === 'image/bmp') return '.bmp'
  return '.img'
}
</script>

<template>
  <main class="relative overflow-hidden">
    <div class="pointer-events-none absolute inset-0 overflow-hidden">
      <div class="absolute left-[-8rem] top-10 h-72 w-72 rounded-full bg-emerald-400/10 blur-3xl animate-drift"></div>
      <div class="absolute right-[-5rem] top-40 h-80 w-80 rounded-full bg-cyan-400/10 blur-3xl animate-drift"></div>
    </div>

    <div class="relative mx-auto flex min-h-screen w-full max-w-6xl flex-col gap-6 px-4 py-6 sm:px-6 lg:px-8">
      <header class="grid gap-4 lg:grid-cols-[1.5fr_1fr]">
        <section class="rounded-[28px] border border-white/10 bg-[color:var(--panel)] p-6 shadow-glow backdrop-blur-xl sm:p-8">
          <p class="mb-3 inline-flex rounded-full border border-emerald-400/20 bg-emerald-400/10 px-3 py-1 text-xs uppercase tracking-[0.3em] text-emerald-200">
            LocalDrop
          </p>
          <h1 class="font-display text-3xl font-semibold tracking-tight text-slate-50 sm:text-5xl">
            局域网里的轻量级同步流
          </h1>
          <p class="mt-4 max-w-2xl text-sm leading-7 text-slate-300 sm:text-base">
            把文本、图片和文件直接推送到同一条时间轴里，桌面和手机都能随手拿起就用。上传、下载、复制、置顶和清理都在一个页面完成。
          </p>

          <div class="mt-6 flex flex-wrap items-center gap-3 text-sm text-slate-300">
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1">轮询刷新: 3 秒</span>
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1">无账号 / 无 PIN</span>
            <span class="rounded-full border border-white/10 bg-white/5 px-3 py-1">本地文件存储 + SQLite</span>
          </div>
        </section>

        <section class="grid gap-3">
          <article
            v-for="card in statCards"
            :key="card.label"
            class="rounded-[24px] border border-white/10 bg-[color:var(--panel-strong)] p-5 backdrop-blur-xl"
          >
            <p class="text-xs uppercase tracking-[0.25em] text-slate-400">{{ card.label }}</p>
            <p class="mt-3 text-3xl font-semibold text-slate-50">{{ card.value }}</p>
            <p class="mt-2 text-sm text-slate-400">{{ card.detail }}</p>
          </article>
        </section>
      </header>

      <section class="grid gap-6 lg:grid-cols-[1.05fr_1.4fr]">
        <div class="space-y-4">
          <article class="rounded-[28px] border border-white/10 bg-[color:var(--panel)] p-5 backdrop-blur-xl sm:p-6">
            <div class="flex items-start justify-between gap-4">
              <div>
                <p class="text-xs uppercase tracking-[0.25em] text-slate-400">Control Deck</p>
                <h2 class="mt-2 text-2xl font-semibold text-slate-50">同步入口</h2>
                <p class="mt-2 text-sm leading-6 text-slate-400">
                  优先读取剪贴板，失败时会自动切换到粘贴回退模式。图片和常见文件都可以直接拖进下面的区域。
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
                class="rounded-[22px] bg-emerald-400 px-5 py-4 text-left text-slate-950 transition hover:bg-emerald-300 disabled:cursor-not-allowed disabled:opacity-60"
                :disabled="syncingClipboard || uploadingFile"
                @click="syncClipboard"
              >
                <span class="block text-xs uppercase tracking-[0.3em] text-slate-900/70">Clipboard</span>
                <span class="mt-2 block text-lg font-semibold">
                  {{ syncingClipboard ? '正在读取...' : '同步剪贴板内容' }}
                </span>
              </button>

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

            <div
              v-if="pasteFallbackVisible"
              class="mt-4 rounded-[24px] border border-amber-300/20 bg-amber-300/10 p-4 text-sm text-amber-50 animate-rise"
            >
              <p class="font-medium">剪贴板回退模式已开启</p>
              <p class="mt-2 leading-6 text-amber-50/80">
                请在下方输入框中执行系统粘贴。文本会直接创建记录，图片会作为新图片上传。
              </p>
              <textarea
                ref="pasteTarget"
                class="mt-3 min-h-24 w-full rounded-2xl border border-amber-100/20 bg-slate-950/40 p-3 text-sm text-slate-50 outline-none ring-0 placeholder:text-slate-500"
                placeholder="在这里按下系统粘贴..."
                @paste="handlePaste"
              ></textarea>
            </div>
          </article>

          <article
            v-if="notice || errorMessage || lastSyncedAt"
            class="rounded-[24px] border border-white/10 bg-[color:var(--panel)] p-5 backdrop-blur-xl"
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

        <section class="rounded-[28px] border border-white/10 bg-[color:var(--panel)] p-5 backdrop-blur-xl sm:p-6">
          <div class="flex flex-wrap items-center justify-between gap-4">
            <div>
              <p class="text-xs uppercase tracking-[0.25em] text-slate-400">Feed Stream</p>
              <h2 class="mt-2 text-2xl font-semibold text-slate-50">信息流</h2>
            </div>
            <div class="flex flex-wrap items-center gap-3">
              <div class="flex items-center gap-2 rounded-full border border-white/10 bg-white/[0.04] px-3 py-2">
                <input
                  v-model="filterQuery"
                  type="text"
                  class="w-40 bg-transparent text-sm text-slate-100 outline-none placeholder:text-slate-500 sm:w-56"
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
                class="rounded-full border border-white/10 bg-white/5 px-4 py-2 text-sm text-slate-200 transition hover:bg-white/10"
                @click="refreshAll"
              >
                立即刷新
              </button>
            </div>
          </div>

          <div v-if="loading" class="mt-8 rounded-[24px] border border-white/10 bg-white/[0.03] px-5 py-12 text-center text-slate-400">
            正在加载 LocalDrop 信息流...
          </div>

          <div v-else-if="records.length === 0" class="mt-8 rounded-[24px] border border-white/10 bg-white/[0.03] px-5 py-12 text-center">
            <p class="text-lg font-medium text-slate-100">还没有同步内容</p>
            <p class="mt-2 text-sm text-slate-400">先试试“同步剪贴板内容”或者拖拽一个文件进来。</p>
          </div>

          <div v-else-if="filteredRecords.length === 0" class="mt-8 rounded-[24px] border border-white/10 bg-white/[0.03] px-5 py-12 text-center">
            <p class="text-lg font-medium text-slate-100">没有匹配的记录</p>
            <p class="mt-2 text-sm text-slate-400">试试别的关键字，支持搜索文本内容和文件名。</p>
          </div>

          <div v-else class="mt-6 space-y-4">
            <article
              v-for="record in filteredRecords"
              :key="record.id"
              class="animate-rise rounded-[24px] border p-4 transition hover:-translate-y-0.5 sm:p-5"
              :class="record.isTop ? 'border-emerald-300/30 bg-emerald-400/10' : 'border-white/10 bg-white/[0.03]'"
            >
              <div class="flex flex-wrap items-center gap-2">
                <span
                  class="rounded-full px-3 py-1 text-xs uppercase tracking-[0.25em]"
                  :class="record.contentType === 'image' ? 'bg-cyan-300/15 text-cyan-100' : record.contentType === 'file' ? 'bg-amber-300/15 text-amber-100' : 'bg-slate-200/10 text-slate-200'"
                >
                  {{ typeLabel(record) }}
                </span>
                <span
                  v-if="record.isTop"
                  class="rounded-full bg-emerald-400 px-3 py-1 text-xs font-semibold uppercase tracking-[0.25em] text-slate-950"
                >
                  pinned
                </span>
                <span class="ml-auto text-xs text-slate-400">{{ formatTimestamp(record.createdAt) }}</span>
              </div>

              <div v-if="record.contentType === 'text'" class="mt-4 whitespace-pre-wrap break-words text-sm leading-7 text-slate-100 sm:text-base">
                {{ record.contentBody }}
              </div>

              <button
                v-else-if="isPreviewableImage(record)"
                class="mt-4 block overflow-hidden rounded-[20px] border border-white/10 bg-slate-950/40"
                @click="previewRecord = record"
              >
                <img
                  :src="imageUrl(record)"
                  :alt="`LocalDrop image ${record.id}`"
                  class="max-h-[24rem] w-full object-cover"
                  loading="lazy"
                />
              </button>

              <div
                v-else
                class="mt-4 rounded-[20px] border border-white/10 bg-slate-950/30 p-4"
              >
                <p class="text-sm text-slate-300">文件名</p>
                <p class="mt-1 break-all text-base font-medium text-slate-50">{{ fileLabel(record) }}</p>
                <p v-if="record.mimeType" class="mt-2 text-sm text-slate-400">{{ record.mimeType }}</p>
              </div>

              <div class="mt-4 flex flex-wrap items-center gap-3 text-sm text-slate-400">
                <span>{{ formatBytes(record.fileSize) }}</span>
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
                  v-if="record.contentType === 'text'"
                  class="rounded-full border border-cyan-300/20 bg-cyan-300/10 px-4 py-2 text-sm text-cyan-100 transition hover:bg-cyan-300/20"
                  @click="copyText(record.contentBody)"
                >
                  复制文本
                </button>
                <a
                  v-if="record.contentType !== 'text'"
                  class="rounded-full border border-amber-300/20 bg-amber-300/10 px-4 py-2 text-sm text-amber-100 transition hover:bg-amber-300/20"
                  :href="downloadUrl(record)"
                >
                  下载文件
                </a>
                <button
                  class="rounded-full border border-rose-400/20 bg-rose-400/10 px-4 py-2 text-sm text-rose-100 transition hover:bg-rose-400/20"
                  @click="deleteRecord(record)"
                >
                  删除
                </button>
              </div>
            </article>
          </div>
        </section>
      </section>
    </div>

    <teleport to="body">
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
