// πreader reads the occurence of an integer within the irrational number Pi and returns the index.
package πreader

import (
  "os"
  "fmt"
  "bufio"
)

type Reader struct {
	file *os.File
}

func New(filepath string) (π *Reader) {
  var err error
	π = &Reader{}
	if π.file==nil {
		π.file, err = os.Open(filepath)
		if err != nil {
			panic(err)
		}
	}
	return π
}

func (π *Reader) Close() {
	π.file.Close()
}

func (π *Reader) Index(number string) (index uint64) {
  var (
    buf []byte
    n int
    err error
    reader *bufio.Reader
  )

  numlen := len(number)

	reader = bufio.NewReaderSize(π.file, numlen*2)
	for {
    buf = make([]byte, numlen*2)
    n, err = reader.Read(buf)
    if err!=nil {
      fmt.Println(err)
      break
    }
    if n < numlen {
      break
    }
    fmt.Println(buf)
	}

  return
}
