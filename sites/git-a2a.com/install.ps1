$ErrorActionPreference = "Stop"

$Version = if ($env:GIT_A2A_VERSION) { $env:GIT_A2A_VERSION } else { "latest" }
$InstallDir = if ($env:GIT_A2A_INSTALL_DIR) { $env:GIT_A2A_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\git-a2a" }
$ReleaseBase = if ($env:GIT_A2A_RELEASE_BASE) { $env:GIT_A2A_RELEASE_BASE.TrimEnd("/") } else { "https://github.com/neprel/git-a2a/releases" }
$DryRun = $false

for ($i = 0; $i -lt $args.Count; $i++) {
    switch ($args[$i]) {
        { $_ -in "--version", "-Version" } {
            if ($i + 1 -ge $args.Count) { throw "install.ps1: --version needs a value" }
            $i++; $Version = $args[$i]
        }
        { $_ -like "--version=*" } { $Version = $_.Substring(10) }
        { $_ -in "--dir", "-Dir" } {
            if ($i + 1 -ge $args.Count) { throw "install.ps1: --dir needs a value" }
            $i++; $InstallDir = $args[$i]
        }
        { $_ -like "--dir=*" } { $InstallDir = $_.Substring(6) }
        { $_ -in "--dry-run", "-DryRun" } { $DryRun = $true }
        default { throw "install.ps1: unknown option: $($_)" }
    }
}

$OperatingSystem = if ($env:GIT_A2A_TEST_OS) { $env:GIT_A2A_TEST_OS } else { "windows" }
if ($OperatingSystem -ne "windows") { throw "install.ps1: unsupported operating system: $OperatingSystem" }
$Machine = if ($env:GIT_A2A_TEST_ARCH) { $env:GIT_A2A_TEST_ARCH } else { [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant() }
switch ($Machine) {
    { $_ -in "x64", "amd64", "x86_64" } { $Arch = "amd64" }
    { $_ -in "arm64", "aarch64" } { $Arch = "arm64" }
    default { throw "install.ps1: unsupported architecture: $Machine" }
}

if ([string]::IsNullOrWhiteSpace($Version)) { throw "install.ps1: version must not be empty" }
if ($Version -ne "latest" -and -not $Version.StartsWith("v")) { $Version = "v$Version" }

if ($DryRun) {
    $ShownVersion = if ($Version -eq "latest") { "v<latest>" } else { $Version }
    $Archive = "git-a2a_$($ShownVersion.TrimStart('v'))_windows_$Arch.zip"
    Write-Output "dry-run: resolve $Version from $ReleaseBase"
    Write-Output "dry-run: download and SHA-256 verify $Archive"
    Write-Output "dry-run: install git-a2a.exe to $(Join-Path $InstallDir 'git-a2a.exe')"
    exit 0
}

if ($Version -eq "latest") {
    $Response = Invoke-WebRequest -Uri "$ReleaseBase/latest" -MaximumRedirection 0 -SkipHttpErrorCheck
    $Location = $Response.Headers.Location
    if (-not $Location) { $Location = $Response.BaseResponse.RequestMessage.RequestUri.AbsoluteUri }
    $Version = $Location.TrimEnd("/").Split("/")[-1]
}
if (-not $Version.StartsWith("v")) { throw "install.ps1: could not resolve a release version" }

$Archive = "git-a2a_$($Version.TrimStart('v'))_windows_$Arch.zip"
$Temporary = Join-Path ([System.IO.Path]::GetTempPath()) ("git-a2a-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $Temporary | Out-Null
try {
    $ArchivePath = Join-Path $Temporary $Archive
    $ChecksumsPath = Join-Path $Temporary "checksums.txt"
    Invoke-WebRequest -Uri "$ReleaseBase/download/$Version/$Archive" -OutFile $ArchivePath
    Invoke-WebRequest -Uri "$ReleaseBase/download/$Version/checksums.txt" -OutFile $ChecksumsPath
    $Pattern = "^([A-Fa-f0-9]{64})\s+\*?" + [regex]::Escape($Archive) + "$"
    $Expected = Get-Content $ChecksumsPath | ForEach-Object { if ($_ -match $Pattern) { $Matches[1].ToLowerInvariant() } } | Select-Object -First 1
    if (-not $Expected) { throw "install.ps1: $Archive is absent from checksums.txt" }
    $Actual = (Get-FileHash -Algorithm SHA256 $ArchivePath).Hash.ToLowerInvariant()
    if ($Actual -ne $Expected) { throw "install.ps1: SHA-256 mismatch for $Archive" }
    Expand-Archive -Path $ArchivePath -DestinationPath $Temporary -Force
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item (Join-Path $Temporary "git-a2a.exe") (Join-Path $InstallDir "git-a2a.exe") -Force
    $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $Parts = @($UserPath -split ";" | Where-Object { $_ })
    if ($Parts -notcontains $InstallDir) {
        [Environment]::SetEnvironmentVariable("Path", (($Parts + $InstallDir) -join ";"), "User")
        Write-Output "added $InstallDir to the user PATH; open a new terminal"
    }
    Write-Output "installed git-a2a $Version to $(Join-Path $InstallDir 'git-a2a.exe')"
}
finally {
    Remove-Item -Recurse -Force $Temporary -ErrorAction SilentlyContinue
}
