if [ ! -n "$1" ] ;then
	echo "please input a target version."
	exit
fi
if [ ! -n "$2" ] ;then
	ver_dir="./all_ver"
else
	ver_dir=$2
fi

src_dir=$ver_dir"/"$1
patches_dir="./patches_"$1

if [ ! -e $ver_dir ] ;then
	echo "$ver_dir not exist."
	exit
fi

if [ ! -e $src_dir ] ;then
	echo "$src_dir not exist."
	exit
fi

if [ ! -e $patches_dir ] ;then
	mkdir $patches_dir
fi

echo current version: $1
echo version path: $ver_dir

#generate target version md5 list
(ls -aR $src_dir/ )  > $src_dir.tmp
pre_dir=
while read line ;do 
	if [[ "." != "$line" && ".." != "$line" ]] ;then
		if [ "${line:0-1}" = ":" ] ;then
			pre_dir=${line:0:0-1}
			if [ "${pre_dir:0-1}" != "/" ] ;then
				pre_dir=$pre_dir/
			fi
			#echo "pre_dir: $pre_dir"
		else
			if [ -f $pre_dir$line ] ;then
				#echo "file: $pre_dir$line"
				md5sum $pre_dir$line >> $src_dir.md5
			fi
		fi
	fi
done < $src_dir.tmp

cd $ver_dir
for cur_ver in `ls` ;do
	dst_dir="./$cur_ver"
	
	if [[ -d "./$cur_ver" && "$1" != "$cur_ver" && "./" != "$cur_ver" && "../" != "$cur_ver" ]] ;then
		#echo "dst_dir:$dst_dir"
		#echo "src_dir:$src_dir"
		
		src_ver=$1
		
	  (rsync -rcn --out-format="%n" "./$src_ver/" "./$cur_ver/") | sort | uniq > $dst_dir.tmp
		
		while read line ;do
			file="$src_ver/$line"
			if [ -f $file ] ;then
				echo "$file" >> $dst_dir.txt
				md5sum "$file" >> $dst_dir.updates
			fi
		done < $dst_dir.tmp

		tar -czvf $cur_ver.tar.gz $cur_ver.updates $(cat $dst_dir.txt) $src_ver.md5
		mv $cur_ver.tar.gz "../patches_$1/$cur_ver.tar.gz"
		rm $dst_dir.*
	fi
done
cd ..
rm $src_dir.*

echo "patches are made in $patches_dir"