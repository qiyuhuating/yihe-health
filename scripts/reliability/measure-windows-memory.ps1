param(
    [ValidateRange(1, 1440)][int]$Minutes = 60,
    [ValidateRange(1, 120)][int]$IntervalSeconds = 15,
    [string]$Output = (Join-Path (Get-Location) 'yihe-process-memory.csv')
)

$targetNames = @('yihe', 'yihe-server')
$until = (Get-Date).AddMinutes($Minutes)
$last = @{}
$first = $true

while ((Get-Date) -lt $until) {
    $now = Get-Date
    $processes = @(Get-Process -Name $targetNames -ErrorAction SilentlyContinue)
    foreach ($process in $processes) {
        $key = "$($process.Id):$($process.StartTime.ToFileTimeUtc())"
        $cpuSeconds = $process.CPU
        $cpuPercent = $null
        if ($last.ContainsKey($key)) {
            $elapsed = ($now - $last[$key].At).TotalSeconds
            if ($elapsed -gt 0) {
                $cpuPercent = [math]::Round(100 * ($cpuSeconds - $last[$key].CPU) / $elapsed, 2)
            }
        }
        $last[$key] = @{ At = $now; CPU = $cpuSeconds }
        $row = [pscustomobject]@{
            At = $now.ToString('o')
            Process = $process.ProcessName
            PID = $process.Id
            WorkingSetMB = [math]::Round($process.WorkingSet64 / 1MB, 2)
            PrivateBytesMB = [math]::Round($process.PrivateMemorySize64 / 1MB, 2)
            CPUPercentOfOneCore = $cpuPercent
            Handles = $process.HandleCount
            Threads = $process.Threads.Count
        }
        $row | Export-Csv -LiteralPath $Output -NoTypeInformation -Encoding utf8 -Append:(-not $first)
        $first = $false
    }
    if ((Get-Date) -ge $until) { break }
    Start-Sleep -Seconds $IntervalSeconds
}
Write-Output "Saved samples to $Output"
