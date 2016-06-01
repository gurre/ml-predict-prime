# Prime prediction using AWS Machine Learning
This is a project just for fun. Trying to predict primes using machine learning is a compelling case. The overall possibility to research number sequences using machine learning is also very interesting.

> If one could find features of primes an increased probability for prediction should be possible.

## Prerequisites
 - Unix environment with Git and Go.
 - Install and configure the [AWS cli](http://docs.aws.amazon.com/cli/latest/userguide/cli-chap-getting-started.html).
 - Clone this repo. `# git clone https://github.com/gurre/ml-predict-prime`

## Data generation
Primes were generated using [primesieve](https://github.com/kimwalisch/primesieve). Since we use threading we need sorting. The sequence 0 to 1e9 will generate about 50 GiB of training data which hits some practical and economical limitations for this project.
```
git clone https://github.com/gurre/ml-predict-prime
# You may also curl the binary from the releases page
cd ml-predict-prime
primesieve 0 1e9 -t2 -s2048 --print=1 | sort -k 1,1n > data/prime-1e9.txt
primesieve 0 1e9 -t2 -s2048 --print=2 | sort -k 2,2n > data/twin-1e9.txt
primesieve 0 1e9 -t2 -s2048 --print=3 | sort -k 2,2n > data/triplet-1e9.txt
primesieve 0 1e9 -t2 -s2048 --print=4 | sort -k 2,2n > data/quad-1e9.txt
primesieve 0 1e9 -t2 -s2048 --print=5 | sort -k 2,2n > data/penta-1e9.txt
primesieve 0 1e9 -t2 -s2048 --print=6 | sort -k 2,2n > data/sexy-1e9.txt
# Generating the training data takes a while. I used a c4.8xlarge(36 cores) to get it done in a reasonable timeframe, XXX hours.
go run make-training-set.go -json verbosetrainingset.json --csv trainingset.csv
```

## Model 1
