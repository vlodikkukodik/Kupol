plugins {
    java
}

group = "dev.evoday"
version = "1.0.0"

repositories {
    mavenCentral()
    maven("https://repo.papermc.io/repository/maven-public/")
}

dependencies {
    compileOnly("io.papermc.paper:paper-api:1.21.4-R0.1-SNAPSHOT")
    // Paper подгружает HikariCP сам через libraries в plugin.yml.
    compileOnly("com.zaxxer:HikariCP:6.2.1")
    // Есть внутри Paper — нужен только для фильтра паролей в логе.
    compileOnly("org.apache.logging.log4j:log4j-core:2.24.1")
    // Тот же jar кладётся и на Velocity - блокирует /server до входа.
    compileOnly("com.velocitypowered:velocity-api:3.4.0-SNAPSHOT")
}

java {
    toolchain.languageVersion.set(JavaLanguageVersion.of(25))
}

tasks.withType<JavaCompile> {
    options.encoding = "UTF-8"
    // Paper 1.21.4 работает на Java 21, 26.x — на 25; 21 подходит для обоих.
    options.release.set(21)
}

tasks.processResources {
    filteringCharset = "UTF-8"
    filesMatching(listOf("plugin.yml", "velocity-plugin.json")) {
        expand("version" to project.version)
    }
}
