Add-Type -AssemblyName System.Drawing

$root = Get-Location
$png = Join-Path $root "frontend\src\assets\images\icon.png"
$ico1 = Join-Path $root "frontend\src\assets\images\icon.ico"
$ico2 = Join-Path $root "build\windows\icon.ico"

$bmp = [System.Drawing.Bitmap]::FromFile($png)
$icon = [System.Drawing.Icon]::FromHandle($bmp.GetHicon())

$fs = New-Object System.IO.FileStream($ico1, [System.IO.FileMode]::Create)
$icon.Save($fs)
$fs.Close()

$fs = New-Object System.IO.FileStream($ico2, [System.IO.FileMode]::Create)
$icon.Save($fs)
$fs.Close()

$icon.Dispose()
$bmp.Dispose()
