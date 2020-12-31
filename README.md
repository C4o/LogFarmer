# LogFarmer
Real-Time and Event-Driven Log Transfer Module.

我们不生产日志，我们只做日志的实时搬运工。

## 功能

### 事件驱动
使用inotify完成，有日志传日志，15s判断一次当前文件是否已经读完需要换个文件句柄。

### 断点续传
进程退出保存文件偏移量和文件名，保存到本地文件，下次启动优先读对应的偏移量。

### 日志发送
脚本内kafka功能删掉了，就创建生产者和发送信息就行，往kafka.Kchan里打数据就可以。

输出到其他地方也一样，换啥改啥就行。功能在main函数里改伪代码。

### nginx按小时分割日志
再记录个在http{}里生效按小时保存日志的配置，不用在每个server{}里改。

之前喜欢把日志格式化成json，所以预设一下json变量。
```
http{

    map $request_uri $nohealthlog {
        /health/check 0;
        default 1;
    }
    map $time_iso8601 $logdate {
        '~^(?<ymd>\d{4}-\d{2}-\d{2})' $ymd;
        default 'default';
    }
    map $time_iso8601 $hour {
        default '00';
        "~^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})" $4;
    }
    map "" $json {
      default "";
    }
    
    log_format main escape=json '$json';
    access_log "/data0/waf_log/access.$logdate-$hour.log" main if=$nohealthlog;
    
}
```
