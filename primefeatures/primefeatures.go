package main

import (
	"../πreader"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	processedComp   uint64
	processedPrimes uint64
	printed         uint64

	VERSION string // Set by goxc

	movingAvg = NewMovingAverage(100)

	//primes = make(chan uint64)
	work   = make(chan *Composite)
	output = make(chan *Composite, 1000)

	flagStart        = flag.Int64("start", 1, "Start on number")
	flagEnd          = flag.Int64("end", 1e9, "End on number")
	flagJsonFilePath = flag.String("json", "", "Output to file formatted in json")
	flagCsvFilePath  = flag.String("csv", "", "Output to file formatted in csv")
	flagSilent       = flag.Bool("silent", false, "Don't print anything")
	flagCpuProfile   = flag.String("cpuprofile", "", "write cpu profile to file")
	flagPiFile       = flag.String("pi", "data/pi-100mb.txt", "read pi digits from this file. The file needs to be a one-liner. E.g. 3.1415...")
)

type Composite struct {
	Comp                                                                  uint64
	Prime                                                                 byte
	Odd                                                                   byte
	Pi                                                                    uint64
	Sin, Cos                                                              float64
	Zeros, Ones, Twos, Threes, Fours, Fives, Sixes, Sevens, Eights, Nines byte
	// http://bit-player.org/2016/prime-after-prime
	Mod2, Mod3, Mod5, Mod7, Mod11, Mod13, Mod17, Mod23, Mod29, Mod31, Mod37            byte
	Totient1e2, Totient2e3, Totient3e4, Totient4e5, Totient5e6            uint64
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
	runtime.GOMAXPROCS(runtime.NumCPU())

	flag.Parse()

	if *flagCpuProfile != "" {
		f, err := os.Create(*flagCpuProfile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if *flagSilent == false {
		fmt.Printf("Running ml-predict-prime(%s) using %d cores.\n", VERSION, runtime.NumCPU())
	}
	if *flagJsonFilePath == "" && *flagCsvFilePath == "" {
		fmt.Println("Must use either --json or --csv flag. (e.g. --csv=test.csv)")
		os.Exit(1)
	}

	// start generating composites
	go compGenerator(func(composite uint64) {
		c := &Composite{Comp: composite}
		work <- c
	})

	for i := 0; i < runtime.NumCPU()/2; i++ {
		go consumer()
	}

	// Write processed primes to json and/or csv
	go fileWriter()

	if *flagSilent == false {
		go statusPrinter()
	}

	quitsig := make(chan os.Signal)
	signal.Notify(quitsig, syscall.SIGINT, syscall.SIGTERM)

	// Block here until we catch a signal
	fmt.Println(<-quitsig)
	//os.Exit(1)
}

func consumer() {
	var (
		c    *Composite
		open bool
		π    *πreader.Reader
	)

	π = πreader.New(*flagPiFile)

	for {
		c, open = <-work
		if !open {
			break
		}
		t0 := time.Now()

		go func(c *Composite, t time.Time) {
			for _, v := range strconv.FormatUint(c.Comp, 10) {
				switch v {
				case '0':
					c.Zeros++
				case '1':
					c.Ones++
				case '2':
					c.Twos++
				case '3':
					c.Threes++
				case '4':
					c.Fours++
				case '5':
					c.Fives++
				case '6':
					c.Sixes++
				case '7':
					c.Sevens++
				case '8':
					c.Eights++
				case '9':
					c.Nines++
				}
			}

			c.Mod2 = byte(c.Comp % 2)
			c.Mod3 = byte(c.Comp % 3)
			c.Mod5 = byte(c.Comp % 5)
			c.Mod7 = byte(c.Comp % 7)
			c.Mod11 = byte(c.Comp % 11)
			c.Mod13 = byte(c.Comp % 13)
			c.Mod17 = byte(c.Comp % 17)
			c.Mod23 = byte(c.Comp % 23)
			c.Mod29 = byte(c.Comp % 29)
			c.Mod31 = byte(c.Comp % 31)
			c.Mod37 = byte(c.Comp % 37)

			c.Sin, c.Cos = math.Sincos(float64(c.Comp))

			// Get the index of this numbers first occurence within Pi
			c.Pi = π.Index(strconv.FormatUint(c.Comp, 10))

			// ℙ index 1 - 100000
			c.Totient1e2 = φ(2, 29, c.Comp)
			c.Totient2e3 = φ(29, 541, c.Comp)
			c.Totient3e4 = φ(541, 7919, c.Comp)
			c.Totient4e5 = φ(7919, 104729, c.Comp)
			c.Totient5e6 = φ(104729, 1299709, c.Comp)

			if c.Mod2 == 1 && isℙ(c.Comp) {
				c.Prime = 1
				atomic.AddUint64(&processedPrimes, 1)
			} else {
				c.Prime = 0
				atomic.AddUint64(&processedComp, 1)
			}
			movingAvg.Add(time.Now().Sub(t).Seconds())
			output <- c
		}(c, t0)
	}
	π.Close()
}

func statusPrinter() {
	var (
		oldProcessedPrimes, oldProcessedComp, oldPrinted uint64
	)
	tick := time.Tick(time.Second)
	for {
		<-tick
		fmt.Printf("\rProcessed primes: %d /s, Composites: %d /s, Printed: %d /s   [%.3f s/op]      ", processedPrimes-oldProcessedPrimes, processedComp-oldProcessedComp, printed-oldPrinted, movingAvg.Avg())
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
		if _, err := cf.WriteString("Comp,Prime,Odd,Totient1e2,Totient2e3,Totient3e4,Totient4e5,Totient5e6\n"); err != nil {
			panic(err)
		}
	}

	for {
		comp, open := <-output
		if !open {
			break
		}
		atomic.AddUint64(&printed, 1)
		if *flagJsonFilePath != "" {
			cjson, _ := json.Marshal(comp)
			if _, err := jf.Write(append(cjson, []byte("\n")...)); err != nil {
				panic(err)
			}
		}
		if *flagCsvFilePath != "" {
			if _, err := cf.WriteString(fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d\n", comp.Comp, comp.Prime, comp.Odd, comp.Totient1e2, comp.Totient2e3, comp.Totient3e4, comp.Totient4e5, comp.Totient5e6)); err != nil {
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

type cbBigint func(uint64)

func compGenerator(callback cbBigint) {
	for i := *flagStart; i < *flagEnd; i++ {
		callback(uint64(i))
	}
	close(work)
}

// https://github.com/aansel/project_euler_go/blob/0a24b2c41e558d157b9b28aec15d6dca43f2141a/src/problem47.go
func getDecomposition(nb uint64) []uint64 {
	var dec []uint64
	var i uint64

	var divisor2 uint64 = 1
	for nb%2 == 0 {
		nb = nb / 2
		divisor2 *= 2
	}
	if divisor2 > 1 {
		dec = append(dec, divisor2)
	}

	for i = 3; nb > 1; i += 2 {
		var divisor uint64 = 1
		for nb%i == 0 && isℙ(i) {
			nb = nb / i
			divisor *= i
		}
		if divisor > 1 {
			dec = append(dec, divisor)
		}
	}
	return dec
}

func isℙ(nb uint64) bool {
	if nb < 2 {
		return false
	}
	var i uint64
	for i = 2; i <= uint64(math.Sqrt(float64(nb))); i++ {
		if nb%i == 0 {
			return false
		}
	}
	return true
}

// In number theory, Euler's totient function counts the positive integers up to a given integer n that are relatively prime to n. It is written using the Greek letter phi as φ(n) or ϕ(n), and may also be called Euler's phi function.
func φ(start, end, number uint64) (residues uint64) {
	for {
		if end < start || start > number {
			break
		}
		if gcd(start, number) == 1 {
			residues++
			//residues = AppendIfMissing(residues, i)
		}
		start++
	}
	return
}

func gcd(x, y uint64) uint64 {
	for y != 0 {
		x, y = y, x%y
	}
	return x
}

func AppendIfMissing(slice []uint64, i uint64) []uint64 {
	for _, ele := range slice {
		if ele == i {
			return slice
		}
	}
	return append(slice, i)
}

// @author Robin Verlangen
// Moving average implementation for Go

type MovingAverage struct {
	Window      int
	values      []float64
	valPos      int
	slotsFilled bool
}

func (ma *MovingAverage) Avg() float64 {
	var sum = float64(0)
	var c = ma.Window - 1

	// Are all slots filled? If not, ignore unused
	if !ma.slotsFilled {
		c = ma.valPos - 1
		if c < 0 {
			// Empty register
			return 0
		}
	}

	// Sum values
	var ic = 0
	for i := 0; i <= c; i++ {
		sum += ma.values[i]
		ic++
	}

	// Finalize average and return
	avg := sum / float64(ic)
	return avg
}
func (ma *MovingAverage) Add(val float64) {
	// Put into values array
	ma.values[ma.valPos] = val

	// Increment value position
	ma.valPos = (ma.valPos + 1) % ma.Window

	// Did we just go back to 0, effectively meaning we filled all registers?
	if !ma.slotsFilled && ma.valPos == 0 {
		ma.slotsFilled = true
	}
}
func NewMovingAverage(window int) *MovingAverage {
	return &MovingAverage{
		Window:      window,
		values:      make([]float64, window),
		valPos:      0,
		slotsFilled: false,
	}
}
