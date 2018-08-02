echo off
echo stop consul

ps -efww|grep consul |grep -v grep|cut -c 9-15|xargs kill

