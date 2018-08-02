echo off
echo stop HDLJ servers

ps -efww|grep game_server |grep -v grep|cut -c 9-15|xargs kill
echo game_server is stop.

ps -efww|grep web_server |grep -v grep|cut -c 9-15|xargs kill
echo web_server is stop.

ps -efww|grep account_server |grep -v grep|cut -c 9-15|xargs kill
echo account_server is stop.

ps -efww|grep master_server |grep -v grep|cut -c 9-15|xargs kill
echo master_server is stop.

ps -efww|grep db_server |grep -v grep|cut -c 9-15|xargs kill 
echo db_server is stop.

echo HDLJ servers is stop.
