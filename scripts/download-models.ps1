# Downloads the YuNet (face detection) and SFace (face recognition) ONNX
# models from the Apache-2.0 licensed opencv_zoo repository.
param(
    [string]$OutputDir = "models"
)

$ErrorActionPreference = "Stop"
$models = @(
    @{
        Url = "https://github.com/opencv/opencv_zoo/raw/main/models/face_detection_yunet/face_detection_yunet_2023mar.onnx"
        Name = "face_detection_yunet.onnx"
    },
    @{
        Url = "https://github.com/opencv/opencv_zoo/raw/main/models/face_recognition_sface/face_recognition_sface_2021dec.onnx"
        Name = "face_recognition_sface.onnx"
    }
)

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
foreach ($model in $models) {
    $target = Join-Path $OutputDir $model.Name
    if (Test-Path $target) {
        Write-Host "skip existing $($model.Name)"
        continue
    }
    $attempt = 0
    while ($true) {
        $attempt++
        try {
            Write-Host "downloading $($model.Name) (attempt $attempt)"
            Invoke-WebRequest -Uri $model.Url -OutFile $target -UseBasicParsing
            break
        }
        catch {
            if ($attempt -ge 5) {
                throw
            }
            Start-Sleep -Seconds ([math]::Min(15, $attempt * 3))
        }
    }
    $hash = (Get-FileHash -Algorithm SHA256 $target).Hash.ToLowerInvariant()
    Write-Host "$($model.Name) sha256=$hash"
}
Write-Host "models ready in $OutputDir"
