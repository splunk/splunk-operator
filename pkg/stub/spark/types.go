package spark


type SparkInstanceType string

const SPARK_MASTER SparkInstanceType = "spark-master"
const SPARK_WORKER SparkInstanceType = "spark-worker"

func (instanceType SparkInstanceType) ToString() string {
	return string(instanceType)
}
