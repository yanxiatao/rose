<script setup lang="ts">
import { ref } from "vue";
import {
  NUpload,
  NUploadDragger,
  NIcon,
  NText,
  type UploadFileInfo,
} from "naive-ui";
import { CloudUploadOutline } from "@vicons/ionicons5";
import { uploadFile, type UploadResult } from "../api";

const emit = defineEmits<{
  uploaded: [result: UploadResult];
}>();

const uploading = ref(false);
const errorMsg = ref("");
const uploadingCount = ref(0);

async function handleUpload({ file }: { file: UploadFileInfo }) {
  if (!file.file) return;
  uploadingCount.value++;
  uploading.value = true;
  errorMsg.value = "";
  try {
    const result = await uploadFile(file.file);
    emit("uploaded", result);
  } catch (e: any) {
    errorMsg.value = `${file.name}: ${e.message || "上传失败"}`;
  } finally {
    uploadingCount.value--;
    if (uploadingCount.value <= 0) {
      uploadingCount.value = 0;
      uploading.value = false;
    }
  }
}
</script>

<template>
  <div>
    <n-upload
      :custom-request="({ file }) => handleUpload({ file })"
      :show-file-list="false"
      :multiple="true"
    >
      <n-upload-dragger>
        <div style="padding: 20px 0">
          <n-icon size="48" :depth="3">
            <CloudUploadOutline />
          </n-icon>
          <n-text style="display: block; margin-top: 8px; font-size: 16px">
            点击或拖拽文件到此处上传
          </n-text>
          <n-text depth="3" style="font-size: 12px">
            支持多选，可一次上传多个词库文件
          </n-text>
        </div>
      </n-upload-dragger>
    </n-upload>

    <div v-if="uploading" style="margin-top: 8px">
      <n-text depth="3">上传中...</n-text>
    </div>

    <div v-if="errorMsg" style="margin-top: 8px; color: #d03050">
      {{ errorMsg }}
    </div>
  </div>
</template>
