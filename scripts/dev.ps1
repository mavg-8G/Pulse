param([string]$Listen = ':8080')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$goCommand = Get-Command go -ErrorAction SilentlyContinue
$pulseGo = if ($goCommand) { $goCommand.Source } else { Join-Path $env:ProgramFiles 'Go\bin\go.exe' }
if (!(Test-Path -LiteralPath $pulseGo)) { throw 'Instala Go y abre una terminal nueva.' }
$env:CGO_ENABLED = '1'
if (!$env:CC) {
    $zigPath = Join-Path $projectRoot '.tools\zig-x86_64-windows-0.15.2\zig.exe'
    if (Test-Path -LiteralPath $zigPath) {
        $env:CC = '"' + $zigPath + '" cc -target x86_64-windows.win10-gnu'
        $env:ZIG_GLOBAL_CACHE_DIR = Join-Path $projectRoot '.tools\zig-cache'
    } elseif (!(Get-Command gcc -ErrorAction SilentlyContinue)) { throw 'SQLite requiere un compilador C. Configura CC con GCC o Zig.' }
}
Push-Location -LiteralPath (Join-Path $projectRoot 'web')
try {
    if (!(Test-Path -LiteralPath 'node_modules')) { & npm.cmd ci; if ($LASTEXITCODE -ne 0) { throw 'No se pudieron instalar las dependencias.' } }
    & npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw 'Falló la compilación del frontend.' }
} finally { Pop-Location }
New-Item -ItemType Directory -Force .dev, bin | Out-Null
$binary = Join-Path $projectRoot 'bin\pulse.exe'
$buildTimestamp = [DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')
$gitRevision = ''
if ((Get-Command git -ErrorAction SilentlyContinue) -and (Test-Path -LiteralPath '.git')) {
    $gitRevision = (& git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'No se pudo leer la revisión de Git.' }
}
& $pulseGo build -ldflags "-X pulse/internal/buildinfo.BuiltAt=$buildTimestamp -X pulse/internal/buildinfo.Revision=$gitRevision" -o $binary ./cmd/pulse
if ($LASTEXITCODE -ne 0) { throw 'Falló la compilación de Go.' }
if (!(Test-Path -LiteralPath '.dev\pulse_kek')) { & $binary genkeys --out .dev; if ($LASTEXITCODE -ne 0) { throw 'Falló la creación de la clave maestra.' } }
if (!(Test-Path -LiteralPath '.dev\vapid_private')) {
    $vapidOutput = & $binary vapid --out .dev
    if ($LASTEXITCODE -ne 0) { throw 'Falló la creación de claves VAPID.' }
    $publicLine = $vapidOutput | Where-Object { $_ -like 'PULSE_VAPID_PUBLIC_KEY=*' }
    $publicLine.Substring('PULSE_VAPID_PUBLIC_KEY='.Length) | Set-Content -LiteralPath '.dev\vapid_public' -Encoding ascii
}
if (!(Test-Path -LiteralPath '.dev\vapid_public')) { throw 'Falta .dev/vapid_public: guarda ahí la clave pública que devolvió pulse vapid.' }
$env:PULSE_ENV = 'development'
$env:PULSE_LISTEN = $Listen
$pulsePort = ($Listen -split ':')[-1]
$env:PULSE_BASE_URL = "http://localhost:$pulsePort"
$env:PULSE_DB_PATH = Join-Path $projectRoot '.dev\pulse.db'
$env:PULSE_KEK_FILE = Join-Path $projectRoot '.dev\pulse_kek'
$env:PULSE_VAPID_PUBLIC_KEY = (Get-Content -LiteralPath '.dev\vapid_public' -Raw).Trim()
$env:PULSE_VAPID_PRIVATE_FILE = Join-Path $projectRoot '.dev\vapid_private'
$env:PULSE_VAPID_SUBJECT = 'mailto:local@example.com'
$env:TZ = 'America/Guayaquil'
Write-Host "Pulse: $env:PULSE_BASE_URL"
Write-Host 'Invitación local (válida 72 h):'
& $binary invite create
if ($LASTEXITCODE -ne 0) { throw 'Falló la creación de invitación.' }
& $binary serve
