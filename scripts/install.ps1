[CmdletBinding()]
param(
    [string]$Version,
    [switch]$Uninstall,
    [switch]$Yes,
    [switch]$DryRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoOwner = if ($env:TABURA_REPO_OWNER) { $env:TABURA_REPO_OWNER } else { "krystophny" }
$RepoName = if ($env:TABURA_REPO_NAME) { $env:TABURA_REPO_NAME } else { "tabura" }
$ReleaseApiBase = if ($env:TABURA_RELEASE_API_BASE) { $env:TABURA_RELEASE_API_BASE } else { "https://api.github.com/repos/$RepoOwner/$RepoName/releases" }
$SkipBrowser = $env:TABURA_INSTALL_SKIP_BROWSER -eq "1"
$AssumeYes = $Yes.IsPresent -or ($env:TABURA_ASSUME_YES -eq "1")

$InstallRoot = if ($env:TABURA_INSTALL_ROOT) { $env:TABURA_INSTALL_ROOT } else { Join-Path $env:LOCALAPPDATA "tabura" }
$BinaryPath = Join-Path $InstallRoot "tabura.exe"
$DataRoot = Join-Path $InstallRoot "data"
$ProjectDir = Join-Path $DataRoot "project"
$WebDataDir = Join-Path $DataRoot "web-data"
$PiperRoot = Join-Path $DataRoot "piper-tts"
$PiperVenv = Join-Path $PiperRoot "venv"
$ModelDir = Join-Path $PiperRoot "models"
$ScriptDir = Join-Path $DataRoot "scripts"
$PiperScriptPath = Join-Path $ScriptDir "piper_tts_server.py"
$IntentDir = Join-Path $DataRoot "intent-classifier"
$IntentVenv = Join-Path $IntentDir "venv"
$LlmDir = Join-Path $DataRoot "llm"
$LlmModelDir = Join-Path $LlmDir "models"
$LlmSetupScript = Join-Path $ScriptDir "setup-local-llm.sh"
$SkipIntent = $env:TABURA_INSTALL_SKIP_INTENT -eq "1"
$SkipLlm = $env:TABURA_INSTALL_SKIP_LLM -eq "1"

function Write-Log {
    param([string]$Message)
    Write-Host "[tabura-install] $Message"
}

function Throw-InstallError {
    param([string]$Message)
    throw "[tabura-install] ERROR: $Message"
}

function Invoke-Step {
    param([ScriptBlock]$Action, [string]$Display)
    if ($DryRun.IsPresent) {
        Write-Log "[dry-run] $Display"
        return
    }
    & $Action
}

function Confirm-DefaultYes {
    param([string]$Prompt)
    if ($AssumeYes) {
        Write-Log "TABURA_ASSUME_YES accepted: $Prompt"
        return $true
    }
    if (-not [Environment]::UserInteractive) {
        Write-Log "non-interactive session defaults to yes: $Prompt"
        return $true
    }
    $answer = Read-Host "$Prompt [Y/n]"
    return [string]::IsNullOrWhiteSpace($answer) -or ($answer -match '^(?i)y(es)?$')
}

function Normalize-Version {
    param([string]$Raw)
    if (-not $Raw) { return "" }
    $clean = $Raw.TrimStart('v', 'V')
    return "v$clean"
}

function Resolve-Arch {
    if ($env:PROCESSOR_ARCHITECTURE -match 'ARM64') { return "arm64" }
    return "amd64"
}

function Require-Codex {
    $cmd = Get-Command codex -ErrorAction SilentlyContinue
    if (-not $cmd) {
        Throw-InstallError "codex app-server is required but codex is not in PATH"
    }
    return $cmd.Source
}

function Require-Python {
    $python = Get-Command python -ErrorAction SilentlyContinue
    if (-not $python) {
        $python = Get-Command py -ErrorAction SilentlyContinue
    }
    if (-not $python) {
        Throw-InstallError "Python 3.10+ is required"
    }

    $versionOutput = & $python.Source -c "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')"
    if (-not $versionOutput) {
        Throw-InstallError "unable to detect Python version"
    }
    $parts = $versionOutput.Trim().Split('.')
    $major = [int]$parts[0]
    $minor = [int]$parts[1]
    if ($major -lt 3 -or ($major -eq 3 -and $minor -lt 10)) {
        Throw-InstallError "Python 3.10+ is required"
    }
    return $python.Source
}

function Get-Release {
    param([string]$Requested)

    if ($env:TABURA_RELEASE_JSON) {
        return ($env:TABURA_RELEASE_JSON | ConvertFrom-Json)
    }
    if ($DryRun.IsPresent) {
        $tag = if ($Requested) { Normalize-Version $Requested } else { "v0.0.0-test" }
        $plain = $tag.TrimStart('v')
        $arch = Resolve-Arch
        $json = @"
{
  "tag_name": "$tag",
  "assets": [
    {"name":"tabura_${plain}_windows_${arch}.zip","browser_download_url":"https://example.invalid/tabura.zip"},
    {"name":"checksums.txt","browser_download_url":"https://example.invalid/checksums.txt"}
  ]
}
"@
        return ($json | ConvertFrom-Json)
    }

    $url = if ($Requested) {
        "$ReleaseApiBase/tags/$(Normalize-Version $Requested)"
    } else {
        "$ReleaseApiBase/latest"
    }
    return Invoke-RestMethod -Uri $url -Headers @{"Accept"="application/vnd.github+json"}
}

function Get-Asset {
    param($Release, [string]$Name)
    $asset = $Release.assets | Where-Object { $_.name -eq $Name } | Select-Object -First 1
    if (-not $asset) {
        Throw-InstallError "release missing asset $Name"
    }
    return $asset
}

function Ensure-InstallDirectories {
    Invoke-Step -Display "Create install directories" -Action {
        New-Item -ItemType Directory -Force -Path $InstallRoot, $DataRoot, $ProjectDir, $WebDataDir, $PiperRoot, $ModelDir, $ScriptDir, $IntentDir, $LlmDir, $LlmModelDir | Out-Null
    }
}

function Install-Binary {
    param($Release)

    $tag = $Release.tag_name
    if (-not $tag) { Throw-InstallError "release did not provide tag_name" }
    $plainVersion = $tag.TrimStart('v')
    $arch = Resolve-Arch
    $assetName = "tabura_${plainVersion}_windows_${arch}.zip"
    $asset = Get-Asset -Release $Release -Name $assetName

    if ($DryRun.IsPresent) {
        Invoke-Step -Display "Install tabura.exe to $BinaryPath" -Action {}
        return $tag
    }

    $checksumAsset = Get-Asset -Release $Release -Name "checksums.txt"
    $tmpDir = Join-Path ([IO.Path]::GetTempPath()) ("tabura-install-" + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmpDir | Out-Null
    try {
        $zipPath = Join-Path $tmpDir $assetName
        $checksumPath = Join-Path $tmpDir "checksums.txt"
        Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $zipPath
        Invoke-WebRequest -Uri $checksumAsset.browser_download_url -OutFile $checksumPath

        $expected = Select-String -Path $checksumPath -Pattern ("\s$([regex]::Escape($assetName))$") | ForEach-Object { ($_ -split '\s+')[0] } | Select-Object -First 1
        if (-not $expected) {
            Throw-InstallError "checksum entry missing for $assetName"
        }
        $actual = (Get-FileHash -Algorithm SHA256 -Path $zipPath).Hash.ToLowerInvariant()
        if ($actual -ne $expected.ToLowerInvariant()) {
            Throw-InstallError "checksum mismatch for $assetName"
        }

        $extractDir = Join-Path $tmpDir "extract"
        Expand-Archive -Path $zipPath -DestinationPath $extractDir -Force
        $exeSource = Get-ChildItem -Path $extractDir -Filter "tabura.exe" -Recurse | Select-Object -First 1
        if (-not $exeSource) {
            Throw-InstallError "tabura.exe missing in release archive"
        }
        Copy-Item -Path $exeSource.FullName -Destination $BinaryPath -Force

        $piperSource = Get-ChildItem -Path $extractDir -Filter "piper_tts_server.py" -Recurse | Select-Object -First 1
        if (-not $piperSource) {
            Throw-InstallError "scripts/piper_tts_server.py missing in release archive"
        }
        Copy-Item -Path $piperSource.FullName -Destination $PiperScriptPath -Force
    }
    finally {
        Remove-Item -Recurse -Force -Path $tmpDir -ErrorAction SilentlyContinue
    }

    return $tag
}

function Ensure-UserPath {
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $parts = @()
    if ($userPath) {
        $parts = $userPath.Split(';', [System.StringSplitOptions]::RemoveEmptyEntries)
    }
    if ($parts -contains $InstallRoot) {
        return
    }
    if ($DryRun.IsPresent) {
        Write-Log "[dry-run] Add $InstallRoot to user PATH"
        return
    }
    $newPath = (($parts + $InstallRoot) -join ';')
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Log "added $InstallRoot to user PATH"
}

function Setup-Piper {
    Write-Host "=== Piper TTS (GPL, runs as HTTP sidecar) ==="
    Write-Host "Piper TTS will be installed as a local HTTP service."
    Write-Host "License: GPL (isolated via HTTP boundary, does not affect Tabura MIT license)"
    Write-Host "Voice models: en_GB-alan-medium (MIT-compatible)"

    if (-not (Confirm-DefaultYes "Install Piper TTS?")) {
        Write-Log "skipping Piper TTS setup"
        return
    }

    if ($DryRun.IsPresent) {
        Invoke-Step -Display "Create Piper venv and install piper-tts fastapi uvicorn" -Action {}
        Invoke-Step -Display "Download Piper voice model en_GB-alan-medium" -Action {}
        return
    }

    $python = Require-Python
    if (-not (Test-Path $PiperVenv)) {
        & $python -m venv $PiperVenv
    }
    $venvPython = Join-Path $PiperVenv "Scripts\python.exe"
    & $venvPython -m pip install --upgrade pip
    & $venvPython -m pip install piper-tts fastapi uvicorn

    $onnx = Join-Path $ModelDir "en_GB-alan-medium.onnx"
    $json = Join-Path $ModelDir "en_GB-alan-medium.onnx.json"
    if (-not (Test-Path $onnx)) {
        Write-Log "model card: https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_GB/alan/medium/MODEL_CARD"
        Invoke-WebRequest -Uri "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_GB/alan/medium/en_GB-alan-medium.onnx" -OutFile $onnx
        Invoke-WebRequest -Uri "https://huggingface.co/rhasspy/piper-voices/resolve/main/en/en_GB/alan/medium/en_GB-alan-medium.onnx.json" -OutFile $json
    }
}

function Setup-IntentClassifier {
    if ($SkipIntent) {
        Write-Log "skipping intent classifier due to TABURA_INSTALL_SKIP_INTENT=1"
        return
    }
    Write-Host "=== Intent Classifier (local, optional) ==="
    Write-Host "A lightweight intent classifier for system action routing."
    Write-Host "Runs as a local HTTP service on port 8425."

    if (-not (Confirm-DefaultYes "Install intent classifier?")) {
        Write-Log "skipping intent classifier setup"
        return
    }

    if ($DryRun.IsPresent) {
        Invoke-Step -Display "Create intent classifier venv and install dependencies" -Action {}
        return
    }

    $python = Require-Python
    if (-not (Test-Path $IntentVenv)) {
        & $python -m venv $IntentVenv
    }
    $venvPython = Join-Path $IntentVenv "Scripts\python.exe"
    & $venvPython -m pip install --upgrade pip
    & $venvPython -m pip install fastapi uvicorn numpy onnxruntime transformers
}

function Setup-LocalLlm {
    if ($SkipLlm) {
        Write-Log "skipping local LLM due to TABURA_INSTALL_SKIP_LLM=1"
        return
    }
    Write-Host "=== Local LLM (Qwen3 0.6B via llama.cpp, optional) ==="
    Write-Host "A small local language model for intent classification fallback."
    Write-Host "Runs as a local HTTP service on port 8426."
    Write-Host "Requires llama.cpp (llama-server binary)."

    if (-not (Confirm-DefaultYes "Install local LLM service?")) {
        Write-Log "skipping local LLM setup"
        return
    }

    $llamaCmd = Get-Command llama-server -ErrorAction SilentlyContinue
    if (-not $llamaCmd) {
        $wingetCmd = Get-Command winget -ErrorAction SilentlyContinue
        if ($wingetCmd -and (Confirm-DefaultYes "Install llama.cpp via winget?")) {
            if ($DryRun.IsPresent) {
                Invoke-Step -Display "winget install ggml-org.llama-cpp" -Action {}
            } else {
                winget install --id ggml-org.llama-cpp --accept-source-agreements --accept-package-agreements
            }
        } else {
            Write-Log "llama-server not found; install llama.cpp and ensure llama-server is on PATH"
        }
    }

    $modelFile = "Qwen3-0.6B-Q4_K_M.gguf"
    $modelPath = Join-Path $LlmModelDir $modelFile
    if (Test-Path $modelPath) {
        Write-Log "LLM model already present: $modelFile"
    } elseif (Confirm-DefaultYes "Download Qwen3 0.6B model (~400 MB)?") {
        if ($DryRun.IsPresent) {
            Invoke-Step -Display "Download $modelFile" -Action {}
        } else {
            $modelUrl = "https://huggingface.co/lmstudio-community/Qwen3-0.6B-GGUF/resolve/main/Qwen3-0.6B-Q4_K_M.gguf?download=true"
            Invoke-WebRequest -Uri $modelUrl -OutFile $modelPath
        }
    }
}

function Write-TaskFiles {
    param([string]$CodexPath)

    $webCmd = '"' + $BinaryPath + '" server --project-dir "' + $ProjectDir + '" --data-dir "' + $WebDataDir + '" --web-host 127.0.0.1 --web-port 8420 --mcp-host 127.0.0.1 --mcp-port 9420 --app-server-url ws://127.0.0.1:8787 --tts-url http://127.0.0.1:8424'
    $piperCmd = 'set "PIPER_MODEL_DIR=' + $ModelDir + '" && "' + (Join-Path $PiperVenv 'Scripts\python.exe') + '" -m uvicorn piper_tts_server:app --app-dir "' + $ScriptDir + '" --host 127.0.0.1 --port 8424'
    $codexCmd = '"' + $CodexPath + '" app-server --listen ws://127.0.0.1:8787'

    $intentVenvPython = Join-Path $IntentVenv "Scripts\python.exe"
    $intentCmd = 'set "PYTHONUNBUFFERED=1" && "' + $intentVenvPython + '" -m uvicorn main:app --app-dir "' + $IntentDir + '" --host 127.0.0.1 --port 8425'

    $llamaPath = (Get-Command llama-server -ErrorAction SilentlyContinue)
    $llmCmd = ""
    if ($llamaPath) {
        $llmCmd = '"' + $llamaPath.Source + '" -m "' + (Join-Path $LlmModelDir "Qwen3-0.6B-Q4_K_M.gguf") + '" --host 127.0.0.1 --port 8426 -c 2048 --threads 4 -ngl 99'
    }

    if ($DryRun.IsPresent) {
        Write-Log "[dry-run] Register scheduled tasks tabura-web, tabura-piper-tts, tabura-codex-app-server, tabura-intent, tabura-llm"
        return
    }

    schtasks /Create /SC ONLOGON /TN "tabura-codex-app-server" /TR $codexCmd /F | Out-Null
    schtasks /Create /SC ONLOGON /TN "tabura-piper-tts" /TR ("cmd /c " + $piperCmd) /F | Out-Null
    schtasks /Run /TN "tabura-codex-app-server" | Out-Null
    schtasks /Run /TN "tabura-piper-tts" | Out-Null

    if (Test-Path $intentVenvPython) {
        schtasks /Create /SC ONLOGON /TN "tabura-intent" /TR ("cmd /c " + $intentCmd) /F | Out-Null
        schtasks /Run /TN "tabura-intent" | Out-Null
    }

    if ($llmCmd) {
        schtasks /Create /SC ONLOGON /TN "tabura-llm" /TR $llmCmd /F | Out-Null
        schtasks /Run /TN "tabura-llm" | Out-Null
    }

    schtasks /Create /SC ONLOGON /TN "tabura-web" /TR $webCmd /F | Out-Null
    schtasks /Run /TN "tabura-web" | Out-Null
}

function Print-WindowsVoxtypeNotice {
    Write-Log "Speech-to-text requires voxtype (Linux/macOS only)"
}

function Open-Browser {
    if ($SkipBrowser) {
        Write-Log "skipping browser open due to TABURA_INSTALL_SKIP_BROWSER=1"
        return
    }
    if ($DryRun.IsPresent) {
        Write-Log "[dry-run] Start browser at http://127.0.0.1:8420"
        return
    }
    Start-Process "http://127.0.0.1:8420" | Out-Null
}

function Print-Summary {
    param([string]$Tag)
    Write-Host ""
    Write-Host "Install complete"
    Write-Host "  Version:      $Tag"
    Write-Host "  Binary:       $BinaryPath"
    Write-Host "  Data root:    $DataRoot"
    Write-Host "  Project dir:  $ProjectDir"
    Write-Host "  Piper models: $ModelDir"
    Write-Host "  Web URL:      http://127.0.0.1:8420"
}

function Remove-Task {
    param([string]$TaskName)
    if ($DryRun.IsPresent) {
        Write-Log "[dry-run] Delete scheduled task $TaskName"
        return
    }
    schtasks /Delete /TN $TaskName /F | Out-Null 2>$null
}

function Uninstall-Tabura {
    Remove-Task "tabura-web"
    Remove-Task "tabura-llm"
    Remove-Task "tabura-intent"
    Remove-Task "tabura-piper-tts"
    Remove-Task "tabura-codex-app-server"

    if ($DryRun.IsPresent) {
        Write-Log "[dry-run] Remove $BinaryPath"
    } else {
        Remove-Item -Force -ErrorAction SilentlyContinue -Path $BinaryPath
    }

    if (Confirm-DefaultYes "Remove $DataRoot data directory?") {
        if ($DryRun.IsPresent) {
            Write-Log "[dry-run] Remove $DataRoot"
        } else {
            Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -Path $DataRoot
        }
    }

    Write-Log "uninstall complete"
}

function Install-Tabura {
    $codexPath = Require-Codex
    Require-Python | Out-Null
    Ensure-InstallDirectories
    $release = Get-Release -Requested $Version
    $tag = Install-Binary -Release $release
    Ensure-UserPath
    Setup-Piper
    Setup-IntentClassifier
    Setup-LocalLlm
    Print-WindowsVoxtypeNotice
    Write-TaskFiles -CodexPath $codexPath
    Open-Browser
    Print-Summary -Tag $tag
}

if ($Uninstall.IsPresent) {
    Uninstall-Tabura
} else {
    Install-Tabura
}
