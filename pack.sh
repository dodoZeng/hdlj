#copy file
var=`date +%Y%m%d`
dir_tmp=hdlj_$var
dir_src=./bin
dir_dst=$1/$dir_tmp
dir_cur=$(pwd)
if [ ! -d $dir_dst ]; then
	mkdir -p $dir_dst
fi

cp -r $dir_src/consul_conf $dir_dst/
cp -r $dir_src/table/ $dir_dst/
cp -i $dir_src/*.cfg $dir_dst
if [ ! -f $dir_dst/roisoft.playerid.ini ]; then
	cp $dir_src/roisoft.playerid.ini $dir_dst/
fi

cp $dir_src/db_server $dir_dst
cp $dir_src/account_server $dir_dst
cp $dir_src/master_server $dir_dst
cp $dir_src/web_server $dir_dst
cp $dir_src/game_server $dir_dst

cp $dir_src/linux/*.service $dir_dst

cp ./install.sh $dir_dst
cp ./uninstall.sh $dir_dst

#tar
cd $1
tar -cvf $dir_tmp.tar $dir_tmp/*
rm -rf $dir_tmp
cd $dir_cur

