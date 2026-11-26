<template>
<el-form ref="form" label-width="80px" :model="draft" :rules="rules" >

        <el-form-item label="手表Id" prop="deviceId">
          <el-input v-model="draft.deviceId" :disabled="editing"></el-input>
        </el-form-item>
    <el-form-item label="血压高值" prop="bpf">
    <el-input v-model="draft.bpf"></el-input>
    </el-form-item>
    <el-form-item label="血压低值" prop="bpl">
    <el-input v-model="draft.bpl"></el-input>
    </el-form-item>
    <el-form-item label="心率高值" prop="hrf">
    <el-input v-model="draft.hrf"></el-input>
    </el-form-item>
    <el-form-item label="心率低值" prop="hrl">
    <el-input v-model="draft.hrl"></el-input>
    </el-form-item>
      </el-form>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { FormInstance } from 'element-plus'
defineProps({ draft: { type: Object, required: true }, rules: Object, editing: Boolean })
const form = ref<FormInstance | null>(null)
defineExpose({
  validate: (...args: Parameters<FormInstance['validate']>) => form.value?.validate(...args),
  resetFields: (...args: Parameters<FormInstance['resetFields']>) => form.value?.resetFields(...args)
})
</script>
