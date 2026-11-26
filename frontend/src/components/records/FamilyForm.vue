<template>
<el-form ref="form" label-position="top" :model="draft" :rules="rules" >
        <el-form-item label="姓名" prop="familyName">
          <el-input v-model="draft.familyName"></el-input>
        </el-form-item>

        <el-form-item label="关系" prop="familyRelation">
          <el-input v-model="draft.familyRelation"></el-input>
        </el-form-item>
    <el-form-item label="性别" prop="familySex">
    <el-radio-group v-model="draft.familySex">
      <el-radio value="男">男</el-radio>
      <el-radio value="女">女</el-radio>
    </el-radio-group>
    </el-form-item>
    <el-form-item label="电话" prop="familyPhone">
    <el-input v-model="draft.familyPhone" inputmode="tel" autocomplete="tel"></el-input>
    </el-form-item>
    <el-form-item label="地址" prop="familyAddress">
    <el-input v-model="draft.familyAddress"></el-input>
    </el-form-item>
    <el-form-item v-if="!editing" label="老人电话" prop="oldPhone">
    <el-input v-model="draft.oldPhone" inputmode="tel" autocomplete="tel"></el-input>
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
