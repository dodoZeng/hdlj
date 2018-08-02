echo off
echo start HDLJ servers

#consul agent -dev -config-dir ../consul_conf -bind=0.0.0.0 &

systemctl start roisoft.db.0.service
systemctl start hdlj.db.0.service
systemctl start roisoft.master.service
systemctl start hdlj.master.test1.service
systemctl start hdlj.account.1.service
systemctl start hdlj.web.captcha.test1.service
systemctl start hdlj.game.test1.service

