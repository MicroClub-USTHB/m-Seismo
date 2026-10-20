package io.mseismo.android

import java.net.URL

data class Configuration(
 val endpoint: URL,
 val projectKey:String,
 val environment:String="production",
 val release :String?=null,
 val shakeEnabled:Boolean = true,
 val screenShotEnabled: Boolean = false,
 )