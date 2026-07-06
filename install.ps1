#!/usr/bin/env pwsh
# Install script for Inference Gateway CLI (Windows)
#
# Usage:
#   .\install.ps1                           # Install latest version
#   .\install.ps1 -Version v0.1.0           # Install specific version
#   $env:INSTALL_DIR = "C:\tools"; .\install.ps1  # Custom install directory

param(
    [string]$Version = "",
    [string]$InstallDir = ""
)

$GithubRepo = "inference-gateway/cli"
$BinaryName = "infer"

if ($InstallDir -eq "") {
    $InstallDir = $env:INSTALL_DIR
    if ($InstallDir -eq "") {
        $InstallDir = "$env:LOCALAPPDATA\Programs\infer"
    }
}

function Write-Status {
    param([string]$Message)
    Write-Host "[INFO] $Message" -ForegroundColor Blue
}

function Write-Success {
    param([string]$Message)
    Write-Host "[SUCCESS] $Message" -ForegroundColor Green
}

function Write-Warning {
    param([string]$Message)
    Write-Host "[WARNING] $Message" -ForegroundColor Yellow
}

function Write-Error {
    param([string]$Message)
    Write-Host "[ERROR] $Message" -ForegroundColor Red
}

function Get-Architecture {
    $arch = (Get-CimInstance Win32_Processor | Select-Object -First 1).AddressWidth
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") {
        return "arm64"
    }
    return "amd64"
}

function Get-LatestVersion {
    $apiUrl = "https://api.github.com/repos/$GithubRepo/releases/latest"
    try {
        $response = Invoke-RestMethod -Uri $apiUrl -Method Get
        return $response.tag_name
    } catch {
        Write-Error "Failed to fetch latest version: $_"
        exit 1
    }
}

function Install-CLI {
    Write-Status "Installing $BinaryName CLI for Windows..."

    $arch = Get-Architecture
    Write-Status "Detected architecture: $arch"

    if ($Version -eq "") {
        $Version = Get-LatestVersion
    }

    Write-Status "Installing version: $Version"

    $filename = "${BinaryName}-windows-${arch}"
    $downloadUrl = "https://github.com/$GithubRepo/releases/download/$Version/$filename"

    Write-Status "Download URL: $downloadUrl"

    $tempDir = Join-Path $env:TEMP "infer-install"
    New-Item -ItemType Directory -Force -Path $tempDir | Out-Null

    $tempFile = Join-Path $tempDir $filename

    try {
        Write-Status "Downloading $filename..."
        Invoke-WebRequest -Uri $downloadUrl -OutFile $tempFile -UseBasicParsing
    } catch {
        Write-Error "Failed to download $filename`: $_"
        Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
        exit 1
    }

    if (-not (Test-Path $InstallDir)) {
        Write-Status "Creating installation directory: $InstallDir"
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    }

    $targetPath = Join-Path $InstallDir "$BinaryName.exe"
    Write-Status "Installing binary to $targetPath..."

    try {
        Move-Item -Force $tempFile $targetPath
    } catch {
        Write-Error "Failed to install binary to $targetPath`: $_"
        Write-Warning "Try running as Administrator or set InstallDir to a writable directory"
        Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue
        exit 1
    }

    Remove-Item -Recurse -Force $tempDir -ErrorAction SilentlyContinue

    if (Test-Path $targetPath) {
        Write-Success "$BinaryName CLI installed successfully!"
        Write-Status "Installed to: $targetPath"

        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if ($userPath -notlike "*$InstallDir*") {
            Write-Warning "$InstallDir is not in your PATH"
            Write-Status "Add the following line to your PowerShell profile:"
            Write-Host "  `$env:Path += `";$InstallDir`""
            Write-Status ""
            Write-Status "Or run this command to add it temporarily:"
            Write-Host "  `$env:Path += `";$InstallDir`""
        } else {
            Write-Status "You can now run: $BinaryName --help"
        }
    } else {
        Write-Error "Installation verification failed"
        exit 1
    }
}

# Banner
Write-Host @"
 ___        __
|_ _|_ __  / _| ___ _ __ ___ _ __   ___ ___
 | || '_ \| |_ / _ \ '__/ _ \ '_ \ / __/ _ \
 | || | | |  _|  __/ | |  __/ | | | (_|  __/
|___|_| |_|_|  \___|_|  \___|_| |_|\___\___|

   ____       _
  / ___| __ _| |_ _____      ____ _ _   _
 | |  _ / _` | __/ _ \ \ /\ / / _` | | | |
 | |_| | (_| | ||  __/\ V  V / (_| | |_| |
  \____|\__,_|\__\___| \_/\_/ \__,_|\__, |
                                     |___/
"@ -ForegroundColor Blue

Install-CLI
