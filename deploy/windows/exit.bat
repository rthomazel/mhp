@echo off
REM Windows exit client launcher for MHP.
REM
REM Launches the exit role: it connects outbound to the relay, reconnects
REM across network loss, and opens the website TCP connections the browser's
REM traffic is forwarded through. It runs unprivileged (no elevation needed).
REM
REM Requires mhp.exe next to this file, cross-built with:
REM   CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o mhp.exe ./cmd/mhp

cd /d "%~dp0"

if not exist mhp.exe (
    echo mhp.exe not found in %~dp0
    echo Build it with: CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o mhp.exe ./cmd/mhp
    pause
    exit /b 1
)

REM <RELAY_IP> = the relay's fixed public IP/hostname.
REM -tls-name is the marker SNI the relay cert was issued for.
REM -ca is the public CA. ca.key stays offline; never copy it here.
REM -token-file is the exit role token (distinct from proxy.token).

mhp.exe -mode exit-node -relay <RELAY_IP>:443 -tls-name en.zalando.de ^
    -ca relay-ca.crt -token-file exit.token -debug

REM Console must stay open. Ctrl+C stops the process cleanly.
REM Retries are handled by the Go process, not this BAT.
pause
