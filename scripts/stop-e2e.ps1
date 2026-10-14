param([string]$Directory = '')
$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$testRoot = [IO.Path]::GetFullPath((Join-Path $projectRoot 'web\.e2e'))
$testDirectory = if ($Directory) { [IO.Path]::GetFullPath($Directory).TrimEnd('\','/') } else { $testRoot }
if ($testDirectory -ne $testRoot -and !$testDirectory.StartsWith($testRoot + '\', [StringComparison]::OrdinalIgnoreCase)) { throw 'Ruta E2E fuera del proyecto.' }
$testBinary = [IO.Path]::GetFullPath((Join-Path $testDirectory 'pulse.exe'))
# Solo detiene el binario del entorno E2E de este proyecto.
Get-Process -Name pulse -ErrorAction SilentlyContinue | ForEach-Object {
    if ($_.Path -and [IO.Path]::GetFullPath($_.Path) -eq $testBinary) { Stop-Process -Id $_.Id -Force }
}
