@echo off
REM Browser-side proxy client launcher for MHP.
REM
REM Exposes SOCKS5 on 127.0.0.1:1080 for Firefox. Point the browser at this
REM loopback port; this process connects outbound to the relay.

cd /d "%~dp0"

if not exist mhp.exe (
    echo mhp.exe not found in %~dp0
    echo Build it with: CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o mhp.exe ./cmd/mhp
    pause
    exit /b 1
)

REM <RELAY_IP> = the relay's fixed public IP/hostname.
REM SOCKS5 is bound to loopback only; no other host can reach it.

mhp.exe -mode proxy -relay <RELAY_IP>:443 -tls-name en.zalando.de ^
    -ca relay-ca.crt -token-file proxy.token -listen 127.0.0.1:1080 -debug

pause
