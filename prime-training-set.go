package main

import (
  "time"
	"bufio"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
  "sync/atomic"
	"syscall"
  "flag"
)



var (
  MIN_RANGE = big.NewInt(1)
  MAX_RANGE = big.NewInt(1e9)

  processedComp uint64
  processedPrimes uint64
  printed uint64

	VERSION string // Set by goxc

	ZERO = big.NewInt(0)
	ONE  = big.NewInt(1)

	// Lookup table: prime -> [degrees of k-tuples...] Use that RAM!
	lookup      = make(map[string][]int)
	lookupmutex = &sync.Mutex{}

  flagJsonFilePath = flag.String("json", "", "Output to file formatted in json")
  flagCsvFilePath = flag.String("csv", "", "Output to file formatted in csv")
)

type Composite struct {
	Comp     *big.Int   `json:"comp"`
	Factors  []*big.Int `json:"factors"`
	Nfactors int        `json:"nfactors"`
	Prime    bool       `json:"prime"`
	Twin     bool       `json:"twin"`
	Triplet  bool       `json:"triplet"`
	Quad     bool       `json:"quad"`
	Penta    bool       `json:"penta"`
	Sexy     bool       `json:"sexy"`
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
  if *flagJsonFilePath=="" && *flagCsvFilePath=="" {
    fmt.Println("Must use either --json or --csv flag.")
    os.Exit(1)
  }

  fmt.Printf("Running prime-training-set %s\n", VERSION)

	runtime.GOMAXPROCS(runtime.NumCPU())

	files := map[string]string{
		"twin":    "data/twin-1e9.txt",
		"triplet": "data/triplet-1e9.txt",
		"quad":    "data/quad-1e9.txt",
		"penta":   "data/penta-1e9.txt",
		"sexy":    "data/sexy-1e9.txt",
	}

	primes := make(chan *big.Int, 10000)
  //compchan := make(chan *Composite, 1000)
  work := make(chan *Composite, 10000)
  output := make(chan *Composite, 10000)



	var wg sync.WaitGroup
  fmt.Printf("Building tuples index...")
	for _, file := range files {
    wg.Add(1)
    go func(file string) {
      //fmt.Printf("Reading: %s -> lookup-map", file)
			forLineInFile(file, func(numbers []string) {
        //fmt.Printf("%+v\n", numbers)
				for _, n := range numbers {
					lookupmutex.Lock()
					lookup[n] = AppendIfMissing(lookup[n], len(numbers))
					lookupmutex.Unlock()
				}
			})
      wg.Done()
      //fmt.Printf("Done: %s -> lookup-map\n", file)
		}(file)
	}


	// Waiting for index to be built

	wg.Wait()
  fmt.Printf("done\n")

  // start generating composites
  go compGenerator(func(composite *big.Int){
    //fmt.Printf("\rGenerated comp %s  ", composite.String())
    c := &Composite{Comp: composite}
    work <- c
  })

	// Primes coming from the files needs preprocessing
	go func() {
    //fmt.Println("Running: primes -> work")
    var (
      c *Composite
      prime *big.Int
      open bool
    )

    prime, _ = <-primes

		for {
      c, open = <-work
      if !open {
				fmt.Println("Work chan closed.")
				return
			}
      //fmt.Printf("Work %s (prime: %s)\n", c.Comp.String(), prime.String())

      // We got a prime
      if prime.Cmp(c.Comp) == 0 {
        //fmt.Printf("\rFound prime %s(%s)", c.Comp.String(), prime.String())
        prime, open = <-primes
				if !open {
					fmt.Println("Primes chan closed.")
					return
				}
        c.Prime = true
      }

      if c.Prime {
        go func(c *Composite){
          c.Factors = make([]*big.Int,0)
          c.Nfactors = 1
  				if tuples, ok := lookup[c.Comp.String()]; ok {
            //fmt.Printf("%s has tuples %v\n", prime.String(), tuples)
  					for _, tuple := range tuples {
  						if tuple == 2 {
  							c.Twin = true
  						}
  						if tuple == 3 {
  							c.Triplet = true
  						}
  						if tuple == 4 {
  							c.Quad = true
  						}
  						if tuple == 5 {
  							c.Penta = true
  						}
  						if tuple == 6 {
  							c.Sexy = true
  						}
  					}
  				}
          go atomic.AddUint64(&processedPrimes, 1)
          output <- c
        }(c)

      }else{
        go func(c *Composite){
          cp := new(big.Int)
          cp.Set(c.Comp)
          c.Factors = Primes(cp)
          c.Nfactors = len(c.Factors)
          go atomic.AddUint64(&processedComp, 1)
          output <- c
        }(c)
      }
		}
	}()

	go func() {
    //fmt.Printf("Running: output -> json:%s csv:%s\n", *flagJsonFilePath, *flagCsvFilePath)
    var (
      jf *os.File
      cf *os.File
      err error
    )

    if *flagJsonFilePath!="" {
      //fmt.Printf("JSON: Training set is stored to %s\n", *flagJsonFilePath)
      jf, err = os.OpenFile(*flagJsonFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
      if err != nil {
        panic(err)
      }
    }

    if *flagCsvFilePath!="" {
      //fmt.Printf("CSV: Training set is stored to %s\n", *flagCsvFilePath)
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

      atomic.AddUint64(&printed, 1)

      if *flagJsonFilePath!="" {
        cjson, _ := json.Marshal(comp)
        if _, err := jf.Write(append(cjson,[]byte("\n")...)); err != nil {
          panic(err)
        }
      }

      if *flagCsvFilePath!="" {
        if _, err := cf.WriteString(fmt.Sprintf("%v\n",comp)); err != nil {
          panic(err)
        }
      }
		}
    if *flagCsvFilePath!="" {
      cf.Close()
    }
    if *flagJsonFilePath!="" {
      jf.Close()
    }
	}()

	// Start feed primes
  //fmt.Println("Reading primes")
	go forLineInFile("data/prime-1e9.txt", func(numbers []string) {
		for _, n := range numbers {
			i := new(big.Int)
			i.SetString(n, 10)
			primes <- i
		}
	})

  go func(){
    var (
      oldProcessedPrimes, oldProcessedComp, oldPrinted uint64
    )
    tick := time.Tick(time.Second)
    for {
      <- tick
      fmt.Printf("\rProcessed primes: %d /s, Composites: %d /s, Printed: %d /s Queues[Pri: %d Calc: %d, Print: %d]         ", processedPrimes-oldProcessedPrimes, processedComp-oldProcessedComp, printed-oldPrinted, len(primes), len(work), len(output))
      oldProcessedPrimes, oldProcessedComp, oldPrinted = processedPrimes, processedComp, printed
    }
  }()

	//fmt.Println("Waiting for routines to finish")
	//wg.Wait()

	quitsig := make(chan os.Signal)
	signal.Notify(quitsig, syscall.SIGINT, syscall.SIGTERM)

	// Block here until we catch a signal
	fmt.Println(<-quitsig)

	/*for k, v := range lookup {
		if len(v) > 4 {
			fmt.Printf("%s -> %v\n", k, v)
		}
	}*/

	os.Exit(1)
}

type cbBigint func(*big.Int)

func compGenerator(callback cbBigint){
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
