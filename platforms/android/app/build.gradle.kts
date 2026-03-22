plugins {
    id("com.android.application")
    kotlin("android")
    kotlin("plugin.compose")
}

android {
    namespace = "com.tabura.android"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.tabura.android"
        minSdk = 28
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    buildFeatures {
        compose = true
    }

    composeOptions {
        kotlinCompilerExtensionVersion = "1.5.15"
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

configurations.configureEach {
    resolutionStrategy.eachDependency {
        if (requested.group == "org.apache.commons.io" && requested.name == "commonsIO") {
            useTarget("commons-io:commons-io:2.5")
        }
        if (requested.group == "com.tencent" && requested.name == "mmkv" && requested.version == "1.0.15") {
            useVersion("1.2.15")
        }
    }
}

dependencies {
    implementation("androidx.activity:activity-compose:1.10.1")
    implementation("androidx.compose.foundation:foundation:1.8.0")
    implementation("androidx.compose.material3:material3:1.3.1")
    implementation("androidx.compose.ui:ui:1.8.0")
    implementation("androidx.compose.ui:ui-tooling-preview:1.8.0")
    implementation("androidx.core:core-ktx:1.16.0")
    implementation("androidx.graphics:graphics-core:1.0.4")
    implementation("androidx.input:input-motionprediction:1.0.0-beta01")
    implementation("androidx.ink:ink-authoring:1.0.0")
    implementation("androidx.ink:ink-brush:1.0.0")
    implementation("androidx.ink:ink-rendering:1.0.0")
    implementation("androidx.ink:ink-strokes:1.0.0")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.9.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.9.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-ktx:2.9.0")
    implementation("androidx.webkit:webkit:1.13.0")
    implementation("com.google.android.material:material:1.12.0")
    implementation("com.onyx.android.sdk:onyxsdk-device:1.1.11") {
        exclude(group = "com.android.support")
    }
    implementation("com.onyx.android.sdk:onyxsdk-pen:1.2.1") {
        exclude(group = "com.android.support")
    }
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")

    debugImplementation("androidx.compose.ui:ui-tooling:1.8.0")
    debugImplementation("androidx.compose.ui:ui-test-manifest:1.8.0")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.json:json:20250107")
    androidTestImplementation("androidx.test:core:1.6.1")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")
    androidTestImplementation("androidx.test:rules:1.6.1")
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.uiautomator:uiautomator:2.3.0")
}
