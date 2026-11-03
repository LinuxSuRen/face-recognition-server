// Backend detail phrases map to stable Chinese copy. Unknown phrases fall
// back to the raw detail so failures are never silently swallowed.
const copies = [
  ['no face detected', '未检测到清晰人脸'],
  ['multiple similarly sized faces', '照片中有多张大小接近的人脸，请确保画面中只有本人'],
  ['image_too_blurry', '人脸清晰度不足，请对焦后重拍'],
  ['face_too_small', '人脸在画面中太小，请靠近一些'],
  ['face_area_too_small', '人脸占画面比例过小，请靠近一些'],
  ["employee's existing samples", '与该员工已有样本不一致，请确认是否本人'],
  ['engine is not loaded', '识别引擎未加载，请检查模型文件与启动日志'],
  ['employee_no already exists', '工号已存在'],
  ['exceeds the', '图片超过大小限制'],
  ['not a valid', '图片文件无法解析，请使用 JPEG、PNG 或 WebP 格式'],
  ['must be a JPEG', '请上传 JPEG、PNG 或 WebP 格式的图片'],
]

export function friendlyError(error) {
  const detail = error && error.message ? String(error.message) : ''
  for (const [phrase, copy] of copies) {
    if (detail.includes(phrase)) {
      return copy
    }
  }
  return detail || '操作失败，请稍后重试'
}

// Recognition-specific copy for face quality reasons returned per face.
export function faceReasonText(face) {
  const reasons = (face.quality && face.quality.reasons) || []
  const reason = face.reason || ''
  if (reasons.includes('image_too_blurry')) return '人脸清晰度不足'
  if (reasons.includes('face_too_small') || reasons.includes('face_area_too_small')) return '人脸过小'
  if (reason === 'empty_face_library') return '人脸库中没有可用样本'
  if (reason === 'below_threshold') return `与最相似员工仅 ${formatScore(face.similarity)}，未达识别阈值`
  if (reason === 'poor_quality') return '图片质量不合格，无法提取特征'
  return ''
}

// Engine unavailability copy keyed by the stable /healthz reason codes.
const engineReasonCopies = {
  no_cgo_build:
    '识别引擎未加载：当前构建不包含推理引擎（缺少 CGO）。人员与人脸管理仍可用；如需识别，请使用官方容器镜像或按 README 构建完整版。',
  models_missing:
    '识别引擎未加载：模型文件缺失。请在服务端运行 scripts/download-models 下载 YuNet 与 SFace 模型后重启服务。',
  ort_library_failed:
    '识别引擎未加载：onnxruntime 动态库加载失败。请检查 FACE_RECOGNITION_ORT_LIB 指向的共享库，版本需与 onnxruntime_go 的要求一致。',
  engine_start_failed: '识别引擎启动失败，详情请查看服务启动日志。',
}

export function engineReasonText(reason) {
  return engineReasonCopies[reason] || engineReasonCopies.engine_start_failed
}

export function formatScore(value) {
  if (value === null || value === undefined) return '—'
  return `${Math.round(value * 100)}%`
}
