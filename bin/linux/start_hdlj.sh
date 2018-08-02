echo off
echo start HDLJ servers

../db_server -node_id=roisoft.db.0 -node_port=20000 -db_dir=roisoft.db &
../db_server -node_id=hdlj.db.0 -node_port=40000 -db_dir=hdlj.db &
echo db_server is started.

../master_server -node_id=roisoft.master -node_port=10000 &
../master_server &
echo master_server is started.

../account_server &
echo account_server is started.

../web_server &
echo web_server is started.

../game_server &
echo game_server is started.

echo HDLJ servers is ready.
