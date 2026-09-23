#!/bin/bash -e
#
#  Mint (C) 2017 Minio, Inc.
# PGG Obstor, (C) 2021-2026 PGG, Inc.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

SPOTBUGS_VERSION="4.2.2" ## needed since 8.0.2 release
JUNIT_VERSION="5.11.4"   ## JUnit Jupiter (JUnit 5) version
OBSTOR_JAVA_VERSION=$(curl --retry 10 -s "https://repo1.maven.org/maven2/io/minio/minio/maven-metadata.xml" | sed -n "/<latest>/{s/<.[^>]*>//g;p;q}" | sed "s/  *//g")
if [ -z "$OBSTOR_JAVA_VERSION" ]; then
	echo "unable to get latest obstor-java version from maven"
	exit 1
fi

test_run_dir="$TESTS_RUN_CORE_DIR/obstor-java"
git clone --quiet https://github.com/obstor/obstor-java.git "$test_run_dir/obstor-java.git"
(
	cd "$test_run_dir/obstor-java.git"
	git checkout --quiet "tags/${OBSTOR_JAVA_VERSION}"

	# Fix CORS configuration comparison - serialize to XML for comparison since CORSConfiguration lacks equals()
	# The Xml.marshal() method converts objects to XML strings, enabling proper equality comparison
	sed -i.bak '/Assertions\.assertEquals(/{N;/"cors: expected: "/s/expectedConfig, config, "cors:/Xml.marshal(expectedConfig), Xml.marshal(config), "cors:/;}' functional/FunctionalTest.java
)
$WGET --output-document="$test_run_dir/minio-${OBSTOR_JAVA_VERSION}-all.jar" "https://repo1.maven.org/maven2/io/minio/minio/${OBSTOR_JAVA_VERSION}/minio-${OBSTOR_JAVA_VERSION}-all.jar"
$WGET --output-document="$test_run_dir/minio-admin-${OBSTOR_JAVA_VERSION}-all.jar" "https://repo1.maven.org/maven2/io/minio/minio-admin/${OBSTOR_JAVA_VERSION}/minio-admin-${OBSTOR_JAVA_VERSION}-all.jar"
$WGET --output-document="$test_run_dir/spotbugs-annotations-${SPOTBUGS_VERSION}.jar" "https://repo1.maven.org/maven2/com/github/spotbugs/spotbugs-annotations/${SPOTBUGS_VERSION}/spotbugs-annotations-${SPOTBUGS_VERSION}.jar"
$WGET --output-document="$test_run_dir/junit-jupiter-api-${JUNIT_VERSION}.jar" "https://repo1.maven.org/maven2/org/junit/jupiter/junit-jupiter-api/${JUNIT_VERSION}/junit-jupiter-api-${JUNIT_VERSION}.jar"
$WGET --output-document="$test_run_dir/junit-platform-commons-1.11.4.jar" "https://repo1.maven.org/maven2/org/junit/platform/junit-platform-commons/1.11.4/junit-platform-commons-1.11.4.jar"
$WGET --output-document="$test_run_dir/opentest4j-1.3.0.jar" "https://repo1.maven.org/maven2/org/opentest4j/opentest4j/1.3.0/opentest4j-1.3.0.jar"
javac -cp "$test_run_dir/minio-${OBSTOR_JAVA_VERSION}-all.jar:$test_run_dir/minio-admin-${OBSTOR_JAVA_VERSION}-all.jar:$test_run_dir/spotbugs-annotations-${SPOTBUGS_VERSION}.jar:$test_run_dir/junit-jupiter-api-${JUNIT_VERSION}.jar:$test_run_dir/junit-platform-commons-1.11.4.jar:$test_run_dir/opentest4j-1.3.0.jar" "${test_run_dir}/minio-java.git/functional"/*.java
cp -a "${test_run_dir}/minio-java.git/functional"/*.class "$test_run_dir/"
rm -fr "$test_run_dir/minio-java.git"
