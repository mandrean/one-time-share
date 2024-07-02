exec_name=one-time-share
exec_path=${exec_name}
chmod +x ${exec_path}
mkdir -p logs
./${exec_path} 2>> logs/log.txt 1>> logs/errors.txt & disown

