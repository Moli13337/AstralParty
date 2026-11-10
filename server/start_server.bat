@echo off
rem Start the Astral Party private server (TCP 8800 + bundle CDN 8810).
rem Keep this window open while playing.
cd /d "%~dp0server"
server.exe -addr :8800 -cdn :8810 -start-uid 1000000 -skip-tutorial
pause
