echo off
echo stop HDLJ servers

systemctl stop hdlj.game.test1.service
systemctl stop hdlj.account.1.service
systemctl stop hdlj.master.test1.service
systemctl stop hdlj.web.captcha.test1.service
systemctl stop roisoft.master.service
systemctl stop hdlj.db.0.service
systemctl stop roisoft.account.db.0.service

#ps -efww|grep consul |grep -v grep|cut -c 9-15|xargs kill
