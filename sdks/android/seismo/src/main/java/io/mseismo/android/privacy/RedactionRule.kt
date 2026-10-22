package io.mseismo.android.privacy

import  java.util.regex.Pattern

data class RedactionRule(
    val name : String,
    val pattern : Pattern,
    val replacement : String="[REDACTED]"
){
    constructor(
        name : String,
        patternString : String,
        replacement : String="[REDACTED]"
     ):this(
        name = name,
        pattern = Pattern.compile(patternString),
        replacement=replacement
    )

    companion object {
        val defaultRules:List<RedactionRule> = listOf(
            RedactionRule(
                name ="email",
                patternString ="[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\\.[A-Za-z]{2,}"
            ),
            RedactionRule(
                name = "phone",
                patternString= "(?<!\\w)\\+?\\d[\\d\\s().-]{7,}\\d(?!\\w)"
            ),
            RedactionRule(
                name = "credit card",
                patternString="\\b(?:\\d[ -]?){13,16}\\b"
            ),
            RedactionRule(
                name = "bearer-token",
                patternString="(?i)Bearer\\s+[A-Za-z0-9._-]+"
            )
        )
    }
}