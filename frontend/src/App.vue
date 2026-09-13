<script setup lang="ts">
import { ref, computed } from 'vue'
import {
  NConfigProvider, NMessageProvider, NCard, NButton,
  NText, NAlert, NTag, NModal, NSwitch, NInputNumber
} from 'naive-ui'
import FileUpload from './components/FileUpload.vue'
import FormatSelect from './components/FormatSelect.vue'
import CustomFormatDialog from './components/CustomFormatDialog.vue'
import EncoderConfigVue from './components/EncoderConfig.vue'
import FilterConfigVue from './components/FilterConfig.vue'
import {
  useFormats, convertFile, matchFormatByExt, formatSize,
  type UploadResult, type EncoderConfig, type FilterConfig,
  type ConvertResult, type CustomFormatConfig, type FormatInfo
} from './api'

// 格式列表
const { formats } = useFormats()

// 已上传的词库文件（可多个，导出时自动合并）
const uploadResults = ref<UploadResult[]>([])
const inputFormat = ref('')
const outputFormat = ref('')
const encoderConfig = ref<EncoderConfig | null>(null)
const filterConfig = ref<FilterConfig>({
  minLength: 0, maxLength: 0, minFrequency: 0, maxFrequency: 0,
  filterEnglish: false, filterNumber: false, customRules: [],
})

// 自定义格式弹窗
const showCustomDialog = ref(false)
const customDialogTarget = ref<'input' | 'output'>('input')
const inputCustomConfig = ref<CustomFormatConfig | null>(null)
const outputCustomConfig = ref<CustomFormatConfig | null>(null)

// 分割导出选项：按条目数把结果拆成多个文件
const splitEnabled = ref(false)
const splitCount = ref(20000)
const splitSize = computed(() =>
  splitEnabled.value && splitCount.value > 0 ? splitCount.value : 0
)

// 弹窗状态
const showFilterDialog = ref(false)
const showResultDialog = ref(false)
const filterRef = ref<InstanceType<typeof FilterConfigVue> | null>(null)

// 转换状态
const converting = ref(false)
const convertResult = ref<ConvertResult | null>(null)
const errorMsg = ref('')

// 选中的输出格式信息
const selectedOutputFormat = computed<FormatInfo | null>(() => {
  if (outputFormat.value === '__custom__' && outputCustomConfig.value) {
    let kind = 3
    if (outputCustomConfig.value.kind === 'wubi') kind = 2
    else if (outputCustomConfig.value.kind === 'pinyin') kind = 1
    return {
      id: '__custom__',
      name: '自定义格式',
      kind,
      ext: '.txt',
      canImport: true,
      canExport: true,
    }
  }
  return formats.value.find(f => f.id === outputFormat.value) || null
})

// 是否可以转换
const canConvert = computed(() =>
  uploadResults.value.length > 0 && inputFormat.value && outputFormat.value && !converting.value
)

function onUploaded(result: UploadResult) {
  uploadResults.value = [...uploadResults.value, result]
  convertResult.value = null
  errorMsg.value = ''
  // 自动匹配输入格式
  const matched = matchFormatByExt(result.filename, formats.value)
  if (matched) {
    inputFormat.value = matched
  }
}

function removeUploaded(id: string) {
  uploadResults.value = uploadResults.value.filter(f => f.id !== id)
  convertResult.value = null
}

// 文件扩展名自动识别出的格式名
function matchedFormatName(filename: string): string | null {
  const id = matchFormatByExt(filename, formats.value)
  if (!id) return null
  return formats.value.find(f => f.id === id)?.name || null
}

function onCustomSelect(target: 'input' | 'output') {
  customDialogTarget.value = target
  showCustomDialog.value = true
}

function onCustomConfirmed(config: CustomFormatConfig) {
  if (customDialogTarget.value === 'input') {
    inputCustomConfig.value = config
    inputFormat.value = '__custom__'
  } else {
    outputCustomConfig.value = config
    outputFormat.value = '__custom__'
  }
}

async function doConvert() {
  if (!canConvert.value || uploadResults.value.length === 0) return

  converting.value = true
  errorMsg.value = ''
  convertResult.value = null

  try {
    const result = await convertFile({
      fileIds: uploadResults.value.map(f => f.id),
      inputFormat: inputFormat.value,
      outputFormat: outputFormat.value,
      inputCustom: inputFormat.value === '__custom__' ? inputCustomConfig.value || undefined : undefined,
      outputCustom: outputFormat.value === '__custom__' ? outputCustomConfig.value || undefined : undefined,
      encoder: encoderConfig.value || undefined,
      filter: filterConfig.value,
      splitSize: splitSize.value || undefined,
    })
    convertResult.value = result
    showResultDialog.value = true
  } catch (e: any) {
    errorMsg.value = e.message || '转换失败'
  } finally {
    converting.value = false
  }
}

function resetFilter() {
  filterRef.value?.reset()
}
</script>

<template>
  <n-config-provider>
    <n-message-provider>
      <div class="app-container">
        <div class="app-content">
          <h1 class="app-title">蔷薇词库转换</h1>
          <n-text depth="3" style="display: block; text-align: center; margin-bottom: 24px">
            支持多种输入法词库格式互相转换
          </n-text>

          <!-- 第1步：上传文件 -->
          <n-card title="①  上传词库文件（可多选）" size="small" style="margin-bottom: 16px">
            <FileUpload @uploaded="onUploaded" />
            <!-- 已上传文件列表 -->
            <div v-if="uploadResults.length" style="margin-top: 12px; display: flex; flex-direction: column; gap: 6px">
              <div
                v-for="f in uploadResults"
                :key="f.id"
                style="display: flex; align-items: center; gap: 8px"
              >
                <n-tag type="success" size="small">已上传</n-tag>
                <span style="flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
                  {{ f.filename }}
                </span>
                <n-text depth="3" style="font-size: 12px; flex-shrink: 0">
                  {{ formatSize(f.size) }}
                </n-text>
                <n-text v-if="matchedFormatName(f.filename)" depth="3" style="font-size: 12px; flex-shrink: 0">
                  {{ matchedFormatName(f.filename) }}
                </n-text>
                <n-button quaternary size="tiny" type="error" @click="removeUploaded(f.id)">
                  移除
                </n-button>
              </div>
              <n-text v-if="uploadResults.length > 1" depth="3" style="font-size: 12px">
                共 {{ uploadResults.length }} 个词库文件，导出时将自动合并
              </n-text>
            </div>
          </n-card>

          <!-- 第2步：选择输入格式 -->
          <n-card title="②  选择词库格式" size="small" style="margin-bottom: 16px">
            <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px">
              <FormatSelect
                label="输入格式"
                :formats="formats"
                v-model="inputFormat"
                mode="import"
                @custom-select="onCustomSelect('input')"
              />
              <FormatSelect
                label="输出格式"
                :formats="formats"
                v-model="outputFormat"
                mode="export"
                @custom-select="onCustomSelect('output')"
              />
            </div>
            <div v-if="inputFormat === '__custom__' && inputCustomConfig" style="margin-top: 8px">
              <n-tag size="small" :type="inputCustomConfig.kind === 'wubi' ? 'warning' : 'info'">
                已配置自定义输入格式（{{ inputCustomConfig.kind === 'wubi' ? '五笔' : inputCustomConfig.kind === 'pinyin' ? '拼音' : '纯词组' }}）
              </n-tag>
            </div>
            <div v-if="outputFormat === '__custom__' && outputCustomConfig" style="margin-top: 8px">
              <n-tag size="small" :type="outputCustomConfig.kind === 'wubi' ? 'warning' : 'info'">
                已配置自定义输出格式（{{ outputCustomConfig.kind === 'wubi' ? '五笔' : outputCustomConfig.kind === 'pinyin' ? '拼音' : '纯词组' }}）
              </n-tag>
            </div>
          </n-card>

          <!-- 第3步：编码器配置（条件显示） -->
          <EncoderConfigVue
            :output-format="selectedOutputFormat"
            @update:config="encoderConfig = $event"
          />

          <!-- 过滤与分割选项 -->
          <div
            style="margin: 16px 0; display: flex; align-items: center; justify-content: center; gap: 24px; flex-wrap: wrap"
          >
            <n-button @click="showFilterDialog = true" quaternary type="info">
              过滤选项（可选）
            </n-button>
            <div v-if="outputFormat" style="display: flex; align-items: center; gap: 8px">
              <n-switch v-model:value="splitEnabled" size="small" />
              <n-text style="font-size: 13px">按条目数分割</n-text>
              <template v-if="splitEnabled">
                <n-input-number
                  v-model:value="splitCount"
                  size="small"
                  :min="1"
                  :step="1000"
                  style="width: 130px"
                />
                <n-text depth="3" style="font-size: 12px">条/文件（默认 20000）</n-text>
              </template>
            </div>
          </div>

          <!-- 转换按钮 -->
          <div style="text-align: center; margin: 20px 0">
            <n-button
              type="primary"
              size="large"
              :disabled="!canConvert"
              :loading="converting"
              @click="doConvert"
              style="min-width: 200px"
            >
              开始转换
            </n-button>
          </div>

          <!-- 错误信息 -->
          <n-alert v-if="errorMsg" type="error" style="margin-bottom: 16px">
            {{ errorMsg }}
          </n-alert>
        </div>

        <!-- 过滤选项弹窗 -->
        <n-modal v-model:show="showFilterDialog" preset="card" title="过滤选项" style="max-width: 520px">
          <FilterConfigVue ref="filterRef" @update:config="filterConfig = $event" />
          <template #footer>
            <div style="display: flex; justify-content: flex-end; gap: 8px">
              <n-button @click="resetFilter">重置</n-button>
              <n-button type="primary" @click="showFilterDialog = false">确定</n-button>
            </div>
          </template>
        </n-modal>

        <!-- 转换结果弹窗 -->
        <n-modal v-model:show="showResultDialog" preset="card" title="转换结果" style="max-width: 480px">
          <template v-if="convertResult">
            <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 12px">
              <n-tag type="success" size="medium">转换成功</n-tag>
              <n-tag v-if="convertResult.mergedFiles" size="medium" type="info">
                已合并 {{ convertResult.mergedFiles }} 个词库文件
              </n-tag>
            </div>
            <div style="display: flex; flex-direction: column; gap: 8px">
              <div v-if="convertResult.outputFiles && convertResult.outputFiles.length > 1">
                <n-text>输出文件（{{ convertResult.outputFiles.length }} 个）:</n-text>
                <div
                  v-for="p in convertResult.outputFiles"
                  :key="p"
                  style="font-size: 13px; word-break: break-all"
                >
                  {{ p }}
                </div>
              </div>
              <n-text v-else>
                输出文件: <strong>{{ convertResult.outputPath }}</strong>
              </n-text>
              <div style="display: flex; gap: 16px; flex-wrap: wrap">
                <n-text>输入词条: <strong>{{ convertResult.stats.inputEntries }}</strong></n-text>
                <n-text>输出词条: <strong>{{ convertResult.stats.outputEntries }}</strong></n-text>
                <n-text v-if="convertResult.stats.duplicates" depth="3">
                  去重: {{ convertResult.stats.duplicates }}
                </n-text>
                <n-text v-if="convertResult.stats.filteredOut > 0" depth="3">
                  过滤: {{ convertResult.stats.filteredOut }}
                </n-text>
              </div>
            </div>
          </template>
          <template #footer>
            <div style="display: flex; justify-content: flex-end">
              <n-button type="primary" @click="showResultDialog = false">确定</n-button>
            </div>
          </template>
        </n-modal>

        <!-- 自定义格式弹窗 -->
        <CustomFormatDialog
          v-model:show="showCustomDialog"
          :initial-config="customDialogTarget === 'input' ? inputCustomConfig : outputCustomConfig"
          @confirmed="onCustomConfirmed"
        />

        <!-- 页脚 -->
        <div class="app-footer">
          <a href="https://github.com/nopdan/rose" target="_blank" rel="noopener">GitHub: nopdan/rose</a>
        </div>
      </div>
    </n-message-provider>
  </n-config-provider>
</template>

<style>
html, body, #app {
  margin: 0;
  padding: 0;
  min-height: 100vh;
  width: 100vw;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'PingFang SC', 'Microsoft YaHei', sans-serif;
  background: #f0f2f5;
  overflow-x: hidden;
}

* {
  box-sizing: border-box;
}

.app-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 32px 16px;
  min-height: 100vh;
}

.app-content {
  width: 100%;
  max-width: 720px;
}

.app-title {
  text-align: center;
  margin-bottom: 4px;
  font-size: 28px;
  font-weight: 600;
  color: #333;
}

.app-footer {
  text-align: center;
  padding: 24px 0 16px;
  font-size: 13px;
}

.app-footer a {
  color: #999;
  text-decoration: none;
}

.app-footer a:hover {
  color: #666;
  text-decoration: underline;
}
</style>
