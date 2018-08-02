#uninstall service
systemctl stop hdlj.db.0 hdlj.master.test1 hdlj.account.test1 hdlj.web.captcha.test1 hdlj.game.test1
systemctl stop roisoft.db.0 roisoft.master
systemctl stop redis consul

systemctl disable hdlj.db.0 hdlj.master.test1 hdlj.account.test1 hdlj.web.captcha.test1 hdlj.game.test1
systemctl disable roisoft.db.0 roisoft.master
systemctl disable redis consul

rm -f /lib/systemd/system/hdlj.*
rm -f /lib/systemd/system/roisoft.*
rm -f /lib/systemd/system/redis.service /lib/systemd/system/consul.service

systemctl daemon-reload

#remove user
user=roisoft
group=roisoft

userdel  $user
#groupdel $group

#remove file
rm -rf /usr/local/bin/hdlj
