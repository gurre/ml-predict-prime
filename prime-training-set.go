package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	MIN_RANGE = big.NewInt(1)
	MAX_RANGE = big.NewInt(1e9)

	processedComp   uint64
	processedPrimes uint64
	printed         uint64

	VERSION string // Set by goxc

	ZERO = big.NewInt(0)
	ONE  = big.NewInt(1)

	// Lookup table: prime -> [degrees of k-tuples...] Use that RAM!
	lookup      = make(map[string][]int)
	lookupmutex = &sync.Mutex{}

	primes = make(chan *big.Int)
	work   = make(chan *Composite)
	output = make(chan *Composite)

	flagJsonFilePath = flag.String("json", "", "Output to file formatted in json")
	flagCsvFilePath  = flag.String("csv", "", "Output to file formatted in csv")
	flagSilent       = flag.Bool("silent", false, "Don't print anything")

	files = map[string]string{
		"twin":    "data/twin-1e9.txt",
		"triplet": "data/triplet-1e9.txt",
		"quad":    "data/quad-1e9.txt",
		"penta":   "data/penta-1e9.txt",
		"sexy":    "data/sexy-1e9.txt",
	}
)

type Composite struct {
	Comp     *big.Int   `json:"comp"`
	Factors  []*big.Int `json:"factors"`
	Nfactors int        `json:"nfactors"`
	Prime    byte       `json:"prime"`
	Twin     byte       `json:"twin"`
	Triplet  byte       `json:"triplet"`
	Quad     byte       `json:"quad"`
	Penta    byte       `json:"penta"`
	Sexy     byte       `json:"sexy"`
}

type WaitGroupWrapper struct {
	sync.WaitGroup
}

func (w *WaitGroupWrapper) Wrap(cb func()) {
	w.Add(1)
	go func() {
		cb()
		w.Done()
	}()
}

func main() {
	flag.Parse()
	runtime.GOMAXPROCS(runtime.NumCPU())
	if *flagSilent == false {
		fmt.Printf("Running prime-training-set (%s)\n", VERSION)
	}
	if *flagJsonFilePath == "" && *flagCsvFilePath == "" {
		fmt.Println("Must use either --json or --csv flag. (e.g. --csv=test.csv)")
		os.Exit(1)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	var wg sync.WaitGroup
	if *flagSilent == false {
		fmt.Printf("Building tuples index...")
	}
	for _, file := range files {
		wg.Add(1)
		go func(file string) {
			forLineInFile(file, func(numbers []string) {
				for _, n := range numbers {
					lookupmutex.Lock()
					lookup[n] = AppendIfMissing(lookup[n], len(numbers))
					lookupmutex.Unlock()
				}
			})
			wg.Done()
		}(file)
	}

	// Waiting for index to be built
	wg.Wait()

  if *flagSilent == false {
	  fmt.Printf("done\n")
  }

	// start generating composites
	go compGenerator(func(composite *big.Int) {
		c := &Composite{Comp: composite}
		work <- c
	})

	go primeProcessor()

	// Write processed primes to json and/or csv
	go fileWriter()

	// Start feed primes
	go forLineInFile("data/prime-1e9.txt", func(numbers []string) {
		for _, n := range numbers {
			i := new(big.Int)
			i.SetString(n, 10)
			primes <- i
		}
	})

	if *flagSilent == false {
		go statusPrinter()
	}

	quitsig := make(chan os.Signal)
	signal.Notify(quitsig, syscall.SIGINT, syscall.SIGTERM)

	// Block here until we catch a signal
	fmt.Println(<-quitsig)
	os.Exit(1)
}

func statusPrinter() {
	var (
		oldProcessedPrimes, oldProcessedComp, oldPrinted uint64
	)
	tick := time.Tick(time.Second)
	for {
		<-tick
		fmt.Printf("\rProcessed primes: %d /s, Composites: %d /s, Printed: %d /s         ", processedPrimes-oldProcessedPrimes, processedComp-oldProcessedComp, printed-oldPrinted)
		oldProcessedPrimes, oldProcessedComp, oldPrinted = processedPrimes, processedComp, printed
	}
}

func fileWriter() {
	var (
		jf  *os.File
		cf  *os.File
		err error
	)

	if *flagJsonFilePath != "" {
		jf, err = os.OpenFile(*flagJsonFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}
	}

	if *flagCsvFilePath != "" {
		cf, err = os.OpenFile(*flagCsvFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			panic(err)
		}
	}

	for {
		comp, open := <-output
		if !open {
			break
		}
		go atomic.AddUint64(&printed, 1)
		if *flagJsonFilePath != "" {
			cjson, _ := json.Marshal(comp)
			if _, err := jf.Write(append(cjson, []byte("\n")...)); err != nil {
				panic(err)
			}
		}
		if *flagCsvFilePath != "" {
			if _, err := cf.WriteString(fmt.Sprintf("%s,%d,%d,%d,%d,%d,%d,%d\n", comp.Comp.String(), comp.Nfactors, comp.Prime, comp.Twin, comp.Triplet, comp.Quad, comp.Penta, comp.Sexy)); err != nil {
				panic(err)
			}
		}
	}
	if *flagCsvFilePath != "" {
		cf.Close()
	}
	if *flagJsonFilePath != "" {
		jf.Close()
	}
}

func primeProcessor() {
	var (
		c     *Composite
		prime *big.Int
		open  bool
	)
	prime, _ = <-primes
	for {
		c, open = <-work
		if !open {
			return
		}
		// We got a prime
		if prime.Cmp(c.Comp) == 0 {
			prime, open = <-primes
			if !open {
				return
			}
			c.Prime = 1
		}
		if c.Prime == 1 {
			go func(c *Composite) {
				c.Factors = make([]*big.Int, 0)
				c.Nfactors = 1
				if tuples, ok := lookup[c.Comp.String()]; ok {
					for _, tuple := range tuples {
						if tuple == 2 {
							c.Twin = 1
						}
						if tuple == 3 {
							c.Triplet = 1
						}
						if tuple == 4 {
							c.Quad = 1
						}
						if tuple == 5 {
							c.Penta = 1
						}
						if tuple == 6 {
							c.Sexy = 1
						}
					}
				}
				go atomic.AddUint64(&processedPrimes, 1)
				output <- c
			}(c)
		} else {
			go func(c *Composite) {
				cp := new(big.Int)
				cp.Set(c.Comp)
				c.Factors = Primes(cp)
				c.Nfactors = len(c.Factors)
				go atomic.AddUint64(&processedComp, 1)
				output <- c
			}(c)
		}
	}
}

type cbBigint func(*big.Int)

func compGenerator(callback cbBigint) {
	var j *big.Int
	for i := MIN_RANGE; i.Cmp(MAX_RANGE) < 1; i.Add(i, ONE) {
		j = new(big.Int)
		callback(j.Add(i, ONE))
	}
}

type cbStr func([]string)

func forLineInFile(filepath string, action cbStr) {
	file, err := os.Open(filepath)
	if err != nil {
		fmt.Println(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	//i := 0
	for scanner.Scan() {
		numbers := strings.Split(strings.Trim(scanner.Text(), "() "), ", ")
		//fmt.Printf("%+v\n", numbers)
		action(numbers)
	}
	if err := scanner.Err(); err != nil {
		fmt.Println(err)
	}
}

// Ref: https://rosettacode.org/wiki/Prime_decomposition#Go
func Primes(n *big.Int) []*big.Int {
	res := []*big.Int{}
	mod, div := new(big.Int), new(big.Int)
	for i := big.NewInt(2); i.Cmp(n) != 1; {
		div.DivMod(n, i, mod)
		for mod.Cmp(ZERO) == 0 {
			res = append(res, new(big.Int).Set(i))
			n.Set(div)
			div.DivMod(n, i, mod)
		}
		i.Add(i, ONE)
	}
	return res
}

func AppendIfMissing(slice []int, i int) []int {
	for _, ele := range slice {
		if ele == i {
			return slice
		}
	}
	return append(slice, i)
}
