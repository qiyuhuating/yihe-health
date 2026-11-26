@echo off
chcp 65001 >nul
if not exist "%~dp0watch-sim.exe" (
  echo 请将此文件和 watch-sim.exe 放在同一个目录。
  pause
  exit /b 1
)
"%~dp0watch-sim.exe" --local "%~dp0本地数据" --scenario demo --health-samples
pause
