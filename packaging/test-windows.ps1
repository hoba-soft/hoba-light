param([Parameter(Mandatory=$true)][string]$Version, [Parameter(Mandatory=$true)][string]$Arch)
$ErrorActionPreference = 'Stop'
function Invoke-Msi([string]$Operation, [string]$Package) {
    $log = Join-Path $env:RUNNER_TEMP ('hobalight-msi-' + [Guid]::NewGuid().ToString() + '.log')
    $packagePath = (Resolve-Path $Package).Path
    $process = Start-Process msiexec.exe -ArgumentList "$Operation `"$packagePath`" /qn /norestart /l*v `"$log`"" -Wait -PassThru
    if ($process.ExitCode -notin @(0, 3010)) {
        Get-Content $log | Write-Host
        throw "MSI failed: $($process.ExitCode)"
    }
}
function Assert-Version([string]$Executable, [string]$Expected) {
    $text = & $Executable --version
    if ($LASTEXITCODE -ne 0 -or $text -notlike "hobalight $Expected-*") { throw "Unexpected version: $text" }
}
$root = Join-Path $env:ProgramFiles 'hobalight'
$previous = "dist-previous/hobalight_0.0.0_windows_$Arch.msi"
$current = "dist/hobalight_${Version}_windows_$Arch.msi"
Invoke-Msi '/i' $previous
Assert-Version (Join-Path $root 'hobalight.exe') '0.0.0'
$profileBefore = $env:USERPROFILE
$testProfile = Join-Path $env:RUNNER_TEMP ('hobalight-profile-' + [Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $testProfile | Out-Null
try {
    $env:USERPROFILE = $testProfile
    & (Join-Path $root 'hobalight.exe') info
    if ($LASTEXITCODE -ne 0) { throw 'Installed executable failed' }
    $config = Join-Path $testProfile '.hobalight/hobalight.conf'
    $configBefore = [IO.File]::ReadAllText($config)
    $marker = Join-Path $testProfile '.hobalight/preserve'
    Set-Content $marker 'keep user data'
    Invoke-Msi '/i' $current
    Assert-Version (Join-Path $root 'hl.exe') $Version
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine') -split ';'
    if (-not ($machinePath | Where-Object { $_.TrimEnd('\') -eq $root })) { throw 'PATH entry missing' }
    if (Test-Path (Join-Path $root 'hobalight.conf')) { throw 'Installer placed an active config beside the executable' }
    if ([IO.File]::ReadAllText($config) -ne $configBefore) { throw 'Upgrade changed config' }
    Invoke-Msi '/x' $current
    if (Test-Path (Join-Path $root 'hobalight.exe')) { throw 'Uninstall left executable' }
    $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine') -split ';'
    if ($machinePath | Where-Object { $_.TrimEnd('\') -eq $root }) { throw 'Uninstall left PATH entry' }
    if ([IO.File]::ReadAllText($config) -ne $configBefore -or -not (Test-Path $marker)) { throw 'Uninstall changed user data' }
} finally {
    $env:USERPROFILE = $profileBefore
    Remove-Item -Recurse -Force $testProfile
}
