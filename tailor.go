package main

import (
	"fmt"
	"os"
	"log"
	"context"
	"os/signal"
	"syscall"
	"time"

	"encoding/gob"
	"github.com/hpcloud/tail"
)

type LogTail struct {
	Path           string
	Offset         int64
	OffsetFileName string
	LogNameFormat  string
	LogFileName    string
	LogFile        *os.File
	LogChan        chan string
	SignalChan     chan os.Signal
	KafkaStatus    bool
}

type OffsetRestore struct {
	Name   string
	Offset int64
}

var (
	// 传递kafka传输开启配置
	KStatus = make(chan int, 64)
	// 调试参数
	PointerAddr string = ""
	TmpOffset   int64  = 0
)

func (lt *LogTail) SaveOffset() {

	var err error
	var offsetFile *os.File
	var or OffsetRestore
	// 打开存放offset的文件
	offsetFile, err = os.OpenFile(lt.OffsetFileName, os.O_CREATE|os.O_WRONLY, 0666)
	defer offsetFile.Close()
	if err != nil {
		log.Printf("open or create offset fail in SignalCatch function : %v", err)
	}

	// 保存offset偏移量到文件
	or.Name = lt.LogFileName
	or.Offset = lt.Offset
	if lt.Offset != TmpOffset {
		log.Printf("OFFSET NOT EQUAL! PointerAddr in SaveOffset is %+v, PointerAddr in Tail is %+v", &lt, PointerAddr)
		//} else {
		//logger.ERR(logger.ERROR, "EQUAL.... PointerAddr in SaveOffset is %+v, PointerAddr in Tail is %+v", &lt, PointerAddr)
	}
	log.Printf("write to offset.restore. name is %s , offset is %d.", or.Name, or.Offset)
	err = gob.NewEncoder(offsetFile).Encode(or)
	if err != nil {
		log.Printf("gob encode fail : %v", err)
	}
}

func (lt *LogTail) SignalCatch() {

	// 获取信号量
	for {
		select {
		case s := <-lt.SignalChan:
			switch s {
			case syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
				//logger.ERR(logger.ERROR, "signal is %+v", s)
				// 进程退出时，保存offset
				lt.SaveOffset()
				os.Exit(0)
			}
		}
	}
}

func (lt *LogTail) SetLogFileName() {

	var err error
	var or OffsetRestore
	var offsetFile *os.File

	// 初始化变量
	lt.LogFileName = fmt.Sprintf("%saccess.%s.log.ts", lt.Path, time.Now().Format("2006-01-02.15"))
	// 打开存放offset的文件
	offsetFile, err = os.OpenFile(lt.OffsetFileName, os.O_CREATE|os.O_RDONLY, 0666)
	defer offsetFile.Close()
	if err != nil {
		log.Printf("open or create offset fail : %v", err)
		return
	}
	// 先获取offset
	err = gob.NewDecoder(offsetFile).Decode(&or)
	if err != nil {
		log.Printf("gob decode fail : %v", err)
		return
	}
	//logger.ERR(logger.DEBUG, "or.name : %s, or.offset : %d ; lt.Name : %s, lt.Offset : %d", or.Name, or.Offset, lt.LogFileName, lt.Offset)
	// 判断文件名是否相同，如果当前应读取的日志不是offset.restore的存的文件名，就舍弃
	if lt.LogFileName == or.Name {
		lt.Offset = or.Offset
	} else {
		lt.Offset = 0
	}
}

func (lt *LogTail) Tail(channel chan []byte) error {

	// 保留当前打开状态的偏移量
	var lastOffset int64 = 0
	// 保留当前打开状态的小时
	var lastHour string = time.Now().Format("15")
	var err error
	var t *tail.Tail

	s1 := time.NewTicker(1 * time.Second)
	s15 := time.NewTicker(15 * time.Second)
	defer func() {
		s1.Stop()
		s15.Stop()
	}()

	// 开始tail日志
	log.Printf("new tail offset is %d", lt.Offset)
	t, err = tail.TailFile(lt.LogFileName, tail.Config{Location: &tail.SeekInfo{lt.Offset, 0}, Follow: true})
	if err != nil {
		log.Printf("tailFile error is : %v", err)
		return err
	}

	// 根据tail的内容进行发送
	for {
		select {
		// 每秒钟判断下开关状态，如果是就终止for循环并发送信号保存offset至文件
		case <-s1.C:
			if !lt.KafkaStatus {
				log.Printf("kafka status change to false.")
				t.Stop()
				lt.SaveOffset()
				return nil
			}
		case line := <-t.Lines:
			channel <- []byte(line.Text)
			lt.Offset, err = t.Tell()
			// 两个调试参数
			TmpOffset = lt.Offset
			PointerAddr = fmt.Sprintf("%+v", &lt)
			if err != nil {
				log.Printf("attach tail offset fail : %v", err)
			}
		case <-s15.C:
			lt.Offset, err = t.Tell()
			// 两个调试参数
			TmpOffset = lt.Offset
			PointerAddr = fmt.Sprintf("%+v", &lt)
			//logger.ERR(logger.DEBUG, "15s loop lt.offset is %d, lastoffset is %d, lt.logname is %s, tail.logname is %s", lt.Offset, lastOffset, lt.LogFileName, t.Filename)
			if err != nil {
				log.Printf("attach tail offset fail : %v", err)
			}
			if lt.Offset == lastOffset && time.Now().Format("15") != lastHour {
				// 偏移量未变，判断是否是当前小时已经过去
				t.StopAtEOF()
				lt.SetLogFileName()
				// 删除上小时日志文件
				if err = os.Remove(fmt.Sprintf("%saccess.%s.log.ts", lt.Path, time.Now().Add(-(3600 * time.Second)).Format("2006-01-02.15"))); err != nil {
					log.Printf("fail to remove log file of last hour...")
				}
				t, err = tail.TailFile(lt.LogFileName, tail.Config{Location: &tail.SeekInfo{lt.Offset, 0}, Follow: true})
				if err != nil {
					log.Printf("tailFile error is : %v", err)
					return err
				}
				lastOffset = 0
				lastHour = time.Now().Format("15")
			} else {
				// 和上次记录的offset不一致，说明还在发送
				lastOffset = lt.Offset
			}
		}
	}
}

type Kafka struct {
	Kchan   chan []byte
}

func main() {

	// 定时器
	s5 := time.NewTicker(5 * time.Second)
	s10 := time.NewTicker(10 * time.Second)
	defer func() {
		s5.Stop()
		s10.Stop()
	}()

    // 创建kafka实例
	kf := Kafka{
		Kchan: make(chan []byte, 10240),
	}
	w := kafka.CreateWriter(kafka.ProducerConfig{
	})
	msg := make([]kafka.Message, 0)
    // 异步kafka线程发送数据
	go func() {
        kafka.Send()
	}()

	lt := LogTail{
		Path: "/path/",
		OffsetFileName: "/path/access.offset",
		SignalChan: make(chan os.Signal),
		KafkaStatus: true,
	}

	// 生成要读取的日志路径和生成offset
	lt.SetLogFileName()
	// 监听系统信号
	signal.Notify(lt.SignalChan)
	go lt.SignalCatch()

RESTART:
	lt.Tail(kf.Kchan)
	<-s5.C
	lt.SetLogFileName()
	goto RESTART

}
