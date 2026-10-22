package io.mseismo.android.privacy

class Redactor(
    val rules: List<RedactionRule> = RedactionRule.defaultRules,
    val denyList: List<String> = emptyList(),
    val allowList:List<String> = emptyList()
){
    data class Placeholder(val token: String, val original: String)

    fun redact(text:String): String{
        if(text.isEmpty()) return text
        var working = text
        val placeholders = mutableListOf<Placeholder>()

        for((index,allowed) in allowList.withIndex()){
            if(allowed.isNotEmpty() && working.contains(allowed)){
                val placeholderToken = "\uE000ALLOW$index\uE001"
                working = working.replace(allowed,placeholderToken)
                placeholders.add(Placeholder(placeholderToken,allowed))
            }
        }

        for (rule in rules){
            val matcher = rule.pattern.matcher(working)
            working = matcher.replaceAll(rule.replacement)
        }

        for(denied in denyList){
            if(denied.isNotEmpty()){
                working = working.replace(denied,"[REDACTED]")

            }
        }

        for(placeholder in placeholders){
            working = working.replace(placeholder.token,placeholder.original)
        }
        return working
    }
}