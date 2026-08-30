-- sample.hs — fixture for HaskellSegmenter's golden test.
module Main where

import Data.List (sort)

-- Color enumerates the three primary colors.
data Color = Red | Green | Blue

-- factorial computes n! for a non-negative n, by pattern-matching
-- equations that all stay in one card.
factorial :: Integer -> Integer
factorial 0 = 1
factorial n = n * factorial (n - 1)

main :: IO ()
main = do
  let xs = sort [3, 1, 2]
  print xs
  print (factorial 5)
