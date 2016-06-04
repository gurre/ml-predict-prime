# Prime prediction using AWS Machine Learning
This is a project just for fun. Trying to predict primes using machine learning is a compelling case. The overall possibility to research number sequences using machine learning is also very interesting.

> If one could find features of primes an increased probability for prediction should be possible.

## Prerequisites
 - Unix environment with Git and Go.
 - Install and configure the [AWS cli](http://docs.aws.amazon.com/cli/latest/userguide/cli-chap-getting-started.html).
 - Install [primesieve](https://github.com/kimwalisch/primesieve).
 - Clone this repo. `# git clone https://github.com/gurre/ml-predict-prime`

## Data generation
Primes were generated using [primesieve](https://github.com/kimwalisch/primesieve). Since we use threading we need sorting.

```
# You may curl the binary from the releases page and skip all this
git clone https://github.com/gurre/ml-predict-prime
cd ml-predict-prime
bash data/generate-primes.sh
# Generating the training data takes a while. I used a c4.8xlarge(36 cores) to get it done in a reasonable timeframe.
go run make-training-set.go -json verbosetrainingset.json --csv trainingset.csv
```

## Model 1
