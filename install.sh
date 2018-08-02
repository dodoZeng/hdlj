#copy file
dir_src=$1
dir_src_service=$1
dir_install=/usr/local/bin/hdlj
if [ ! $dir_src ]; then
	dir_src="./bin"
	dir_src_service="./bin/linux"
fi
if [ ! -d $dir_src ]; then
	echo "invalid source path: $dir_src"
	exit
fi
if [ ! -d $dir_install ]; then
	mkdir -p $dir_install
fi

cp -r $dir_src/consul_conf $dir_install/
cp -r $dir_src/table/ $dir_install/
cp -i $dir_src/*.cfg $dir_install
if [ ! -f $dir_install/roisoft.playerid.ini ]; then
	cp $dir_src/roisoft.playerid.ini $dir_install/
fi

cp $dir_src/db_server $dir_install
cp $dir_src/account_server $dir_install
cp $dir_src/master_server $dir_install
cp $dir_src/web_server $dir_install
cp $dir_src/game_server $dir_install



#create user
user=roisoft
group=roisoft

#egrep "^$group" /etc/group >& /dev/null
#if [ $? -ne 0 ]
#then
#    groupadd $group
#fi

egrep "^$user" /etc/passwd >& /dev/null
if [ $? -ne 0 ]
then
    useradd -d "/home/$user" -m -s "/bin/bash" $user
fi



#install service
cp $dir_src_service/*.service /lib/systemd/system/
systemctl daemon-reload
systemctl enable redis consul
systemctl enable roisoft.db.0 roisoft.master
systemctl enable hdlj.db.0 hdlj.master.test1 hdlj.account.test1 hdlj.web.captcha.test1 hdlj.game.test1

systemctl restart redis consul
systemctl restart roisoft.db.0 roisoft.master
systemctl restart hdlj.db.0 hdlj.master.test1 hdlj.account.test1 hdlj.web.captcha.test1 hdlj.game.test1
