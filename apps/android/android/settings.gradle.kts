pluginManagement {
    val flutterSdkPath =
        run {
            val properties = java.util.Properties()
            file("local.properties").inputStream().use { properties.load(it) }
            val flutterSdkPath = properties.getProperty("flutter.sdk")
            require(flutterSdkPath != null) { "flutter.sdk not set in local.properties" }
            flutterSdkPath
        }

    // [红果漫剧补丁] 非ASCII路径(容器工作区)会让Gradle 9的included build路径编码错乱
    // (mojibake /workspace/çº¢æ)。优先用ASCII符号链接 /tmp/ft，避免 flutter.sdk 直写。
    // [红果漫剧补丁] 非ASCII工作区路径会让 Gradle 9 included-build 路径编码错乱（本地调试场景）。
    // 本地容器用 /tmp/ft 符号链接规避；CI（GitHub runner 纯 ASCII 路径）走原有逻辑。
    val ftGradle = java.io.File("/tmp/ft/packages/flutter_tools/gradle")
    if (ftGradle.isDirectory) {
        includeBuild(ftGradle.absolutePath)
        extensions.extraProperties.set("flutterSdkPath", "/tmp/ft")
    } else {
        includeBuild("$flutterSdkPath/packages/flutter_tools/gradle")
    }

    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

plugins {
    id("dev.flutter.flutter-plugin-loader") version "1.0.0"
    id("com.android.application") version "9.1.0" apply false
    id("org.jetbrains.kotlin.android") version "2.4.0" apply false
}

include(":app")
