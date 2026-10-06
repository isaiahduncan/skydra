<#
.SYNOPSIS
    Runs skydra on a local kind cluster. The PowerShell version of the Makefile.

.DESCRIPTION
    Wraps the docker, kind and kubectl commands from the README so you can run the
    service on kind from Windows PowerShell without installing make.

    Run it from anywhere. It switches to the repo root first.

.PARAMETER Command
    help      show this help (the default)
    all       up + load + deploy, the quickest way to get running
    up        create the kind cluster (skipped if it already exists)
    load      build the Docker image and load it into the cluster
    deploy    apply the manifests and wait for the rollout
    restart   rebuild, reload and restart the pod after a code change
    logs      follow the pod logs (Ctrl+C to stop)
    status    show the pod
    down      delete the cluster
    test      go test ./...  (add -Race for the race detector, needs gcc)
    vet       go vet ./...
    validate  render and check the manifests (needs bash, kubectl and kubeconform)

.PARAMETER DryRun
    Print the commands without running them.

.PARAMETER Race
    With "test", run the Go tests with the race detector.

.EXAMPLE
    .\scripts\kind.ps1 all
    .\scripts\kind.ps1 logs

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File .\scripts\kind.ps1 all
    Runs it once even if PowerShell blocks scripts on your machine.
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('help', 'all', 'up', 'load', 'deploy', 'restart', 'logs', 'status', 'down', 'test', 'vet', 'validate')]
    [string]$Command = 'help',

    [switch]$DryRun,

    [switch]$Race
)

$ErrorActionPreference = 'Stop'

# These match the Makefile and the manifests. Change them there too if you change them here.
$Image = 'ghcr.io/isaiahduncan/skydra'
$Tag = 'dev'
$Cluster = 'skydra'
$Namespace = 'skydra-dev'
$ImageRef = "${Image}:${Tag}"
$KubeContext = "kind-$Cluster"

Set-Location (Split-Path -Parent $PSScriptRoot)

function Invoke-Native {
    param(
        [string]$Exe,
        [string[]]$Arguments
    )
    Write-Host "> $Exe $($Arguments -join ' ')" -ForegroundColor Cyan
    if ($DryRun) {
        return
    }
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "'$Exe' failed with exit code $LASTEXITCODE"
    }
}

function Assert-Tool {
    param(
        [string]$Name,
        [string]$InstallHint
    )
    if ($DryRun) {
        return
    }
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "'$Name' was not found on PATH. Install it with: $InstallHint (then open a new terminal)"
    }
}

function Assert-DockerRunning {
    if ($DryRun) {
        return
    }
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & docker info *> $null
        $ok = ($LASTEXITCODE -eq 0)
    }
    finally {
        $ErrorActionPreference = $previous
    }
    if (-not $ok) {
        throw 'Docker is not running. Start Docker Desktop and wait until it says it is running, then try again.'
    }
}

function Test-ClusterExists {
    if ($DryRun) {
        return $false
    }
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $names = @(& kind get clusters 2>$null)
    }
    finally {
        $ErrorActionPreference = $previous
    }
    return ($names -contains $Cluster)
}

# kubectl always targets the kind cluster, whatever your current context is.
function Invoke-Kubectl {
    param([string[]]$Arguments)
    Invoke-Native 'kubectl' (@('--context', $KubeContext) + $Arguments)
}

function Invoke-Up {
    Assert-Tool 'docker' 'winget install Docker.DockerDesktop'
    Assert-Tool 'kind' 'winget install Kubernetes.kind'
    Assert-DockerRunning
    if (Test-ClusterExists) {
        Write-Host "Cluster '$Cluster' already exists, skipping create." -ForegroundColor Yellow
        return
    }
    Invoke-Native 'kind' @('create', 'cluster', '--name', $Cluster, '--config', 'k8s/kind/cluster.yaml')
}

function Invoke-Load {
    Assert-Tool 'docker' 'winget install Docker.DockerDesktop'
    Assert-Tool 'kind' 'winget install Kubernetes.kind'
    Assert-DockerRunning
    Invoke-Native 'docker' @('build', '-t', $ImageRef, '.')
    Invoke-Native 'kind' @('load', 'docker-image', $ImageRef, '--name', $Cluster)
}

function Invoke-Deploy {
    Assert-Tool 'kubectl' 'winget install Kubernetes.kubectl'
    Invoke-Kubectl @('apply', '-k', 'k8s/overlays/dev')
    Invoke-Kubectl @('-n', $Namespace, 'rollout', 'status', 'deployment/skydra', '--timeout=120s')
}

function Invoke-Restart {
    Invoke-Load
    Assert-Tool 'kubectl' 'winget install Kubernetes.kubectl'
    Invoke-Kubectl @('-n', $Namespace, 'rollout', 'restart', 'deployment/skydra')
    Invoke-Kubectl @('-n', $Namespace, 'rollout', 'status', 'deployment/skydra', '--timeout=120s')
}

function Invoke-Logs {
    Assert-Tool 'kubectl' 'winget install Kubernetes.kubectl'
    Invoke-Kubectl @('-n', $Namespace, 'logs', '-f', 'deployment/skydra')
}

function Invoke-Status {
    Assert-Tool 'kubectl' 'winget install Kubernetes.kubectl'
    Invoke-Kubectl @('-n', $Namespace, 'get', 'pods')
}

function Invoke-Down {
    Assert-Tool 'kind' 'winget install Kubernetes.kind'
    Invoke-Native 'kind' @('delete', 'cluster', '--name', $Cluster)
}

function Invoke-Test {
    Assert-Tool 'go' 'winget install GoLang.Go'
    $goArgs = @('test')
    if ($Race) {
        $goArgs += '-race'
    }
    $goArgs += './...'
    Invoke-Native 'go' $goArgs
}

function Invoke-Vet {
    Assert-Tool 'go' 'winget install GoLang.Go'
    Invoke-Native 'go' @('vet', './...')
}

function Invoke-Validate {
    Assert-Tool 'bash' 'install Git for Windows (Git Bash) or WSL'
    Invoke-Native 'bash' @('scripts/validate-k8s.sh')
}

function Show-Help {
    Write-Host @'
Usage: .\scripts\kind.ps1 <command> [-DryRun] [-Race]

Runs skydra on a local kind cluster. The PowerShell version of the Makefile.

Commands:
  all       up + load + deploy, the quickest way to get running
  up        create the kind cluster (skipped if it already exists)
  load      build the Docker image and load it into the cluster
  deploy    apply the manifests and wait for the rollout
  restart   rebuild, reload and restart the pod after a code change
  logs      follow the pod logs (Ctrl+C to stop)
  status    show the pod
  down      delete the cluster
  test      go test ./...  (add -Race for the race detector, needs gcc)
  vet       go vet ./...
  validate  render and check the manifests (needs bash, kubectl and kubeconform)
  help      show this help

Options:
  -DryRun   print the commands without running them
  -Race     with "test", run the Go tests with the race detector

Examples:
  .\scripts\kind.ps1 all
  .\scripts\kind.ps1 logs
  powershell -ExecutionPolicy Bypass -File .\scripts\kind.ps1 all
'@
}

try {
    switch ($Command) {
        'help'     { Show-Help }
        'all'      { Invoke-Up; Invoke-Load; Invoke-Deploy
                     Write-Host ''
                     Write-Host 'skydra is deployed. Follow the logs with:  .\scripts\kind.ps1 logs' -ForegroundColor Green }
        'up'       { Invoke-Up }
        'load'     { Invoke-Load }
        'deploy'   { Invoke-Deploy }
        'restart'  { Invoke-Restart }
        'logs'     { Invoke-Logs }
        'status'   { Invoke-Status }
        'down'     { Invoke-Down }
        'test'     { Invoke-Test }
        'vet'      { Invoke-Vet }
        'validate' { Invoke-Validate }
    }
}
catch {
    Write-Host "Error: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
