package main

import (
	fm "file_manager"
	"fmt"
	lm "log_manager"
)

func makeRecord(s string, n uint64) []byte {
	//使用page提供接口来设置字节数组的内容
	p := fm.NewPageBySize(1)
	npos := p.MaxLengthForString(s)
	b := make([]byte, npos+lm.UINT64_LEN)
	p = fm.NewPageByBytes(b)
	p.SetString(0, s)
	p.SetInt(npos, n)
	return b
}

func createRecords(log_lm *lm.LogManager, start uint64, end uint64) {
	for i := start; i <= end; i++ {
		//一条记录包含两个信息，一个是字符串record 一个是数值i
		rec := makeRecord(fmt.Sprintf("record%d", i), i)
		log_lm.Append(rec)
	}
}

func main() {
	file_manager, _ := fm.NewFileManager("logtest", 400)
	log_manager, err := lm.NewLogManager(file_manager, "logfile")

	if err != nil {
		fmt.Println(err)
	}

	//createRecords(log_manager, 1, 35)
	//log_manager.Flush()

	iter := log_manager.Iterator()
	rec_num := 35
	for iter.HasNext() {
		rec := iter.Next()
		p := fm.NewPageByBytes(rec)
		s := p.GetString(0)

		npos := p.MaxLengthForString(s)
		val := p.GetInt(npos)
		fmt.Printf("s:%s, v:%d\n", s, val)
		rec_num -= 1
	}

}
