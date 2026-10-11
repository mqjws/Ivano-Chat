@echo off
setlocal
cd /d E:\Ivano-Chat-dev

if not exist .cache\ivano-stage3\ivano-stage3.exe (
  echo [!] missing .cache\ivano-stage3\ivano-stage3.exe - build it first:
  echo     cd /d E:\Ivano-Chat-dev\backend
  echo     set CGO_CFLAGS=-ID:/MinGW/mingw64/opt/include
  echo     go build -mod=readonly -trimpath -o ..\.cache\ivano-stage3\ivano-stage3.exe ./cmd/server
  exit /b 1
)

start "ivano fake upstream" cmd /c "python scripts\ivano\fake-upstream.py 8099"
set SQLITE_PATH=E:/Ivano-Chat-dev/.cache/ivano-stage3/verify.db
set HTTP_PORT=8080
echo.
echo fake upstream : http://127.0.0.1:8099/v1
echo open in browser: http://localhost:8080/login
echo press Ctrl+C to stop the app; close the fake upstream window separately
echo.
.cache\ivano-stage3\ivano-stage3.exe
