echo off
echo stop HDLJ servers

TASKKILL /F /IM game_server.exe
echo game_server is stop.

TASKKILL /F /IM web_server.exe
echo web_server is stop.

TASKKILL /F /IM account_server.exe
echo account_server is stop.

TASKKILL /F /IM master_server.exe
echo master_server is stop.

TASKKILL /F /IM db_server.exe
echo db_server is stop.

echo HDLJ servers is stop.