package utils

import (
	"fmt"
	"os"
	"strconv"
)

func StringtoInt32(value string) (int32, error) {
	if value == "" {
		return 0, fmt.Errorf("No empty value ")
	}
	int64Value, err := strconv.ParseInt(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("Can not parseInt %s ", value)
	}
	newValue := int32(int64Value)
	if newValue <= 0 {
		return 0, fmt.Errorf("Should not positive number ")
	}
	return newValue, nil
}

func EnvVar(key, defaultValue string) string {
	if os.Getenv(key) == "" {
		return defaultValue
	}
	return os.Getenv(key)
}

// check the list contain string
func IsInSlice(x string, list []*interface{}) bool {
	for _,v := range list {
		if *v == x {
			return true
		}
	}
	return false
}