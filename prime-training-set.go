package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	MIN_RANGE int64
	MAX_RANGE int64

	processedComp   uint64
	processedPrimes uint64
	printed         uint64

	VERSION string // Set by goxc

	// Lookup table: prime -> [degrees of k-tuples...] Use that RAM!
	lookup      = make(map[string][]uint64)
	lookupmutex = &sync.Mutex{}

	movingAvg = NewMovingAverage(100)

	//primes = make(chan uint64)
	work   = make(chan *Composite)
	output = make(chan *Composite, 100000)

	flagStart        = flag.Int64("start", 1, "Start on number")
	flagEnd          = flag.Int64("end", 1e9, "End on number")
	flagJsonFilePath = flag.String("json", "", "Output to file formatted in json")
	flagCsvFilePath  = flag.String("csv", "", "Output to file formatted in csv")
	flagSilent       = flag.Bool("silent", false, "Don't print anything")
	flagCpuProfile   = flag.String("cpuprofile", "", "write cpu profile to file")

	files = map[string]string{
		"twin":    "data/twin-1e9.txt",
		"triplet": "data/triplet-1e9.txt",
		"quad":    "data/quad-1e9.txt",
		"penta":   "data/penta-1e9.txt",
		"sexy":    "data/sexy-1e9.txt",
	}
)

type Composite struct {
	Comp           uint64        `json:"comp"`
	Prime          byte          `json:"prime"`
	Factors        []uint64      `json:"factors"`
	Nfactors       uint64        `json:"nfactors"`
	ReducedResidue []uint64      `json:"residues"` // Just to heavy to compute
	Totient        uint64        `json:"totient"`
	Twin           byte          `json:"twin"`
	Triplet        byte          `json:"trip"`
	Quad           byte          `json:"quad"`
	Penta          byte          `json:"penta"`
	Sexy           byte          `json:"sexy"`
	Duration       time.Duration `json:"nanodura"`
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
		fmt.Printf("Running prime-training-set (%s)\n", VERSION)
	}
	if *flagJsonFilePath == "" && *flagCsvFilePath == "" {
		fmt.Println("Must use either --json or --csv flag. (e.g. --csv=test.csv)")
		os.Exit(1)
	}

	MIN_RANGE = *flagStart
	MAX_RANGE = *flagEnd

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
					lookup[n] = AppendIfMissing(lookup[n], uint64(len(numbers)))
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
	go compGenerator(func(composite uint64) {
		c := &Composite{Comp: composite}
		work <- c
	})

	for i := 0; i < runtime.NumCPU(); i++ {
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
	os.Exit(1)
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
		if _, err := cf.WriteString("Comp,Prime,Nfactors,Totient,Twin,Triplet,Quad,Penta,Sexy\n"); err != nil {
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
			if _, err := cf.WriteString(fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d,%d\n", comp.Comp, comp.Prime, comp.Nfactors, comp.Totient, comp.Twin, comp.Triplet, comp.Quad, comp.Penta, comp.Sexy)); err != nil {
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

func consumer() {
	var (
		c    *Composite
		open bool
	)

	for {
		c, open = <-work
		if !open {
			return
		}
		t0 := time.Now()

		if isPrime(c.Comp) {
			go func(c *Composite, t time.Time) {
				c.Factors = []uint64{}
				c.Nfactors = 0
				c.Prime = 1

				c.Totient = c.Comp - 1 // Quite a lot faster then φ(c.Comp)
				//c.Totient = φ(c.Comp)
				if tuples, ok := lookup[strconv.FormatUint(c.Comp, 10)]; ok {
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
				c.Duration = time.Now().Sub(t)
				movingAvg.Add(c.Duration.Seconds())
				atomic.AddUint64(&processedPrimes, 1)
				output <- c
			}(c, t0)
		} else {
			go func(c *Composite, t time.Time) {
				c.Prime = 0
				c.Factors = getDecomposition(c.Comp)
				c.Nfactors = uint64(len(c.Factors))

				c.Totient = φ(c.Comp)
				//c.ReducedResidue = φ(c.Comp)

				c.Duration = time.Now().Sub(t)
				movingAvg.Add(c.Duration.Seconds())
				atomic.AddUint64(&processedComp, 1)
				output <- c
			}(c, t0)
		}
	}
}

type cbBigint func(uint64)

func compGenerator(callback cbBigint) {
	for i := MIN_RANGE; i < MAX_RANGE; i++ {
		callback(uint64(i))
	}
	close(work)
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
		for nb%i == 0 && isPrime(i) {
			nb = nb / i
			divisor *= i
		}
		if divisor > 1 {
			dec = append(dec, divisor)
		}
	}
	return dec
}

func isPrime(nb uint64) bool {
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
func φ(number uint64) (residues uint64) {
	var i uint64
	for i = 1; i <= number; i++ {
		if gcd(i, number) == 1 {
			residues++
			//residues = AppendIfMissing(residues, i)
		}
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
