任何一个应用只要冠以”系统“二字，那么它一定离不开一个模块，那就是”日志“。既然我们要开发一个数据库系统，那么它必然要有自己的日志模块。日志通常用于记录系统的运行状态，有点类似于快照，一旦系统出现异常，那么管理员或者它的代码本身可以通过扫描分析日志来确定问题所在，或者通过日志执行错误恢复，这点对数据库系统更加重要。

数据库系统经常要往文件中读写大量数据，在这个过程中很容易出现各种各样的问题，例如在执行一个交易时，网络突然断开，机器突然断电，于是交易执行到一半就会突然中断，当系统重新启动时，整个数据库就会处于一种错误状态，也就是有一部数据写入，但还有一部分数据丢失，这种情况对数据库系统而言非常致命，倘若不能保证数据的一致性，那么这种数据系统就不会有人敢使用。那如何保证数据一致性呢，这就得靠日志来保证，数据库在读写数据前，会先写入日志，记录相应的操作，例如当前操作是读还是写，然后记录要读写的数据。假设我们现在有个业务，要把一百行数据写入两个表，前50行写入表1，后50行写入表2，于是日志就会记录”表1写入0到50行“；”表2写入51到100行“，这类信息。假设在数据写入前50行后突然断电，机器重启，数据库系统重新启动后，它自动扫描日志发现”表2写入51到100行“这个操作没有执行，于是再次执行这个操作，这样数据的一致性就能得以保证。

本节我们在上一节实现文件系统的基础上，看看如何实现日志模块。对于日志模块而言，日志就是一组字节数组，它只负责把数组内容写入内存或是磁盘文件，数据到底有什么内容，格式如何解析它一概不管。同时在日志写入时采用”压栈“模式，假设我们有3条日志，其长度分别为50字节，100字节，100字节，现在我们有400字节的缓存可以写入，那么写入日志时我们将从缓存的末尾开始写，例如存入第一条日志时，我们从缓存第350字节开始写入，于是350字节到400字节就对应第一条日志，然后我们把当前可写入的地址放置到缓存的开头8字节，例如第一条日志写入后，下次可写入的地址是350，于是我们在缓存开头8字节存入数据350，当要写入第二条日志时，我们读取缓存前8字节，拿到数值350，由于第二条缓存长度100字节，于是我们将其写入缓存的地址为350-100=250，于是写入后缓存的250到350部分的内容就对应第二条日志，然后我们将250写入缓存开头8字节；当写入第三条日志时，系统读取开头8字节得到数值250，于是第三条日志的写入地址就是250-100=150，于是系统将第三条日志写入缓存偏移150字节处，于是从150字节到250字节这部分的数据就对应第3条日志，同时把150存入开头8字节，以此类推。

废话不多说，我们看看具体代码实现，首先创建文件夹log_manager，加入log_manager.go，输入以下代码：
```
package log_manager

import (
	fm "file_manager"
	"sync"
)

const (
	UINT64_LEN = 8
)

type LogManager struct {
	file_manager   *fm.FileManager
	log_file       string
	log_page       *fm.Page
	current_blk    *fm.BlockId
	latest_lsn     uint64 //当前日志序列号
	last_saved_lsn uint64 //上次存储到磁盘的日志序列号
	mu             sync.Mutex
}

func (l *LogManager) appendNewBlock() (*fm.BlockId, error) {
	blk, err := l.file_manager.Append(l.log_file)
	if err != nil {
		return nil, err
	}
	/*
		添加日志时从内存的底部往上走，例如内存400字节，日志100字节，那么
		日志将存储在内存的300到400字节处，因此我们需要把当前内存可用底部偏移
		写入头8个字节
	*/
	l.log_page.SetInt(0, uint64(l.file_manager.BlockSize()))
	l.file_manager.Write(&blk, l.log_page)
	return &blk, nil
}

func NewLogManager(file_manager *fm.FileManager, log_file string) (*LogManager, error) {
	log_mgr := LogManager{
		file_manager:   file_manager,
		log_file:       log_file,
		log_page:       fm.NewPageBySize(file_manager.BlockSize()),
		last_saved_lsn: 0,
		latest_lsn:     0,
	}

	log_size, err := file_manager.Size(log_file)
	if err != nil {
		return nil, err
	}

	if log_size == 0 { //如果文件为空则添加新区块
		blk, err := log_mgr.appendNewBlock()
		if err != nil {
			return nil, err
		}
		log_mgr.current_blk = blk
	} else { //文件有数据，则在文件末尾的区块读入内存，最新的日志总会存储在文件末尾
		log_mgr.current_blk = fm.NewBlockId(log_mgr.log_file, log_size-1)
		file_manager.Read(log_mgr.current_blk, log_mgr.log_page)
	}

	return &log_mgr, nil
}

func (l *LogManager) FlushByLSN(lsn uint64) error {
	/*
	将给定编号及其之前的日志写入磁盘，注意这里会把与给定日志在同一个区块，也就是Page中的
	日志也写入磁盘。例如调用FlushLSN(65)表示把编号65及其之前的日志写入磁盘，如果编号为
	66,67的日志也跟65在同一个Page里，那么它们也会被写入磁盘
	*/
	if lsn > l.last_saved_lsn {
		err := l.Flush()
		if err != nil {
			return err
		}
		l.last_saved_lsn = lsn
	}

	return nil
}

func (l *LogManager) Flush() error {
	//将当前区块数据写入写入磁盘
	_, err := l.file_manager.Write(l.current_blk, l.log_page)
	if err != nil {
		return err
	}

	return nil
}

func (l *LogManager) Append(log_record []byte) (uint64, error) {
	//添加日志
	l.mu.Lock()
	defer l.mu.Unlock()

	boundary := l.log_page.GetInt(0) //获得可写入的底部偏移
	record_size := uint64(len(log_record))
	bytes_need := record_size + UINT64_LEN
	var err error
	if int(boundary-bytes_need) < int(UINT64_LEN) {
		//当前容量不够,先将当前日志写入磁盘
		err = l.Flush()
		if err != nil {
			return l.latest_lsn, err
		}
		//生成新区块用于写新数据
		l.current_blk, err = l.appendNewBlock()
		if err != nil {
			return l.latest_lsn, err
		}

		boundary = l.log_page.GetInt(0)
	}

	record_pos := boundary - bytes_need         //我们从底部往上写入
	l.log_page.SetBytes(record_pos, log_record) //设置下次可以写入的位置
	l.log_page.SetInt(0, record_pos)
	l.latest_lsn += 1 //记录新加入日志的编号

	return l.latest_lsn, err
}

func (l *LogManager) Iterator() *LogIterator {
	//生成日志遍历器
	l.Flush()
	return NewLogIterator(l.file_manager, l.current_blk)
}

```
上面代码所构造的日志管理器，其作用就是将写入的日志先存储在内存块中，一旦当前内存块写满则将其写入磁盘文件，然后生成新的内存块用于写入新日志。每次日志管理器启动时，它根据给定的目录读取目录下的二进制文件，将文件尾部的区块读入内存，这样就能得到文件存储的日志数据。

问了更好的遍历日志，我们要构造一个日志遍历器，在同一个目录下创建log_iterator.go，然后写入以下内容：
```
package log_manager

import (
	fm "file_manager"
)



/*
LogIterator用于遍历给定区块内的记录,由于记录从底部往上写，因此记录1,2,3,4写入后在区块的排列为
4,3,2,1，因此LogIterator会从上往下遍历记录，于是得到的记录就是4,3,2,1
*/

type LogIterator struct {
	file_manager *fm.FileManager 
	blk         *fm.BlockId 
    p           *fm.Page 
	current_pos uint64 
	boundary    uint64 
}

func NewLogIterator(file_manager *fm.FileManager, blk *fm.BlockId) *LogIterator{
    it := LogIterator{
		file_manager: file_manager,
		blk: blk , 
	}

	//现将给定区块的数据读入
	it.p = fm.NewPageBySize(file_manager.BlockSize())
	err := it.moveToBlock(blk) 
    if err != nil {
		return nil 
	}
	return &it 
}

func (l *LogIterator) moveToBlock(blk *fm.BlockId) error {
	//打开存储日志数据的文件，遍历到给定区块，将数据读入内存
	_, err := l.file_manager.Read(blk, l.p)
	if err != nil {
		return err 
	}

	//获得日志的起始地址
	l.boundary = l.p.GetInt(0)
	l.current_pos = l.boundary
	return nil
}

func (l *LogIterator) Next() []byte {
	//先读取最新日志，也就是编号大的，然后依次读取编号小的
	if l.current_pos == l.file_manager.BlockSize() {
		l.blk = fm.NewBlockId(l.blk.FileName(), l.blk.Number() - 1)
		l.moveToBlock(l.blk)
	}

	record := l.p.GetBytes(l.current_pos)
	l.current_pos += UINT64_LEN + uint64(len(record))

	return record 
}

func (l *LogIterator) HasNext() bool {
	//如果当前偏移位置小于区块大那么还有数据可以从当前区块读取
	//如果当前区块数据已经全部读完，但是区块号不为0，那么可以读取前面区块获得老的日志数据
	return l.current_pos < l.file_manager.BlockSize() || l.blk.Number() > 0
}
```
日志遍历器的作用是逐条读取日志，它先从最新的日志开始读取，然后依次获取老的日志。最后我们通过测试用例来理解当前代码的作用和逻辑，添加log_manager_test.go，代码如下：
```
package log_manager

import (
	fm "file_manager"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func makeRecord(s string, n uint64) []byte {
	//使用page提供接口来设置字节数组的内容
	p := fm.NewPageBySize(1)
	npos := p.MaxLengthForString(s)
	b := make([]byte, npos+UINT64_LEN)
	p = fm.NewPageByBytes(b)
	p.SetString(0, s)
	p.SetInt(npos, n)
	return b
}

func createRecords(lm *LogManager, start uint64, end uint64) {
	for i := start; i <=
		end; i++ {
		//一条记录包含两个信息，一个是字符串record 一个是数值i
		rec := makeRecord(fmt.Sprintf("record%d", i), i)
		lm.Append(rec)
	}
}

func TestLogManager(t *testing.T) {
	file_manager, _ := fm.NewFileManager("logtest", 400)
	log_manager, err := NewLogManager(file_manager, "logfile")
	require.Nil(t, err)

	createRecords(log_manager, 1, 35)

	iter := log_manager.Iterator()
	rec_num := uint64(35)
	for iter.HasNext() {
		rec := iter.Next()
		p := fm.NewPageByBytes(rec)
		s := p.GetString(0)

		require.Equal(t, fmt.Sprintf("record%d", rec_num), s)
		npos := p.MaxLengthForString(s)
		val := p.GetInt(npos)
		require.Equal(t, val, rec_num)
		rec_num -= 1
	}

	createRecords(log_manager, 36, 70)
	log_manager.FlushByLSN(65)

	iter = log_manager.Iterator()
	rec_num = uint64(70)
	for iter.HasNext() {
		rec := iter.Next()
		p := fm.NewPageByBytes(rec)
		s := p.GetString(0)
		require.Equal(t, fmt.Sprintf("record%d", rec_num), s)
		npos := p.MaxLengthForString(s)
		val := p.GetInt(npos)
		require.Equal(t, val, rec_num)
		rec_num -= 1
	}
}

```
用例首先创建日志管理器，然后写入35条日志，每条日志后面又跟着日志的编号。例如第35条日志的内容为"record35"，这个字符串会以字节数组的方式写入到区块中，然后再把读入的数据重新读取，同时判断读取的数据与写入的是否一致。

更详细的讲解和调试演示，请在B站搜索Coding迪斯尼，更多有趣内容请点击[ 这里](http://m.study.163.com/provider/7600199/index.htm?share=2&shareId=7600199)：http://m.study.163.com/provider/7600199/index.htm?share=2&shareId=7600199
