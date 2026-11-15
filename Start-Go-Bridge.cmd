@echo off
setlocal
cd /d "%~dp0"
set "BRIDGE=%~dp0dist\local\deadlock-translator.exe"
if not exist "%BRIDGE%" set "BRIDGE=%~dp0deadlock-translator.exe"
if not exist "%BRIDGE%" set "BRIDGE=%~dp0dist\windows-amd64\deadlock-translator.exe"
if not exist "%BRIDGE%" (
  echo Build or extract the Windows version first. See README.md.
  pause
  exit /b 1
)
"%BRIDGE%" %*
if errorlevel 1 pause
