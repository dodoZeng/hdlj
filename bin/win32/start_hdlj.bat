echo off
echo start HDLJ servers

start /b ../db_server.exe -node_id=roisoft.db.0 -node_port=20000 -db_dir=roisoft.db 
start /b ../db_server.exe -node_id=hdlj.db.0 -node_port=40000 -db_dir=hdlj.db 
echo db_server is started.

start /b ../master_server.exe -node_id=roisoft.master -node_port=10000
start /b ../master_server.exe
echo master_server is started.

start /b ../account_server.exe
echo account_server is started.

start /b ../web_server.exe
echo web_server is started.

start /b ../game_server.exe
echo game_server is started.

echo HDLJ servers is ready.