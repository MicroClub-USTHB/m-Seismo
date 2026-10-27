package io.mseismo.android.privacy

import org.junit.Assert.assertEquals
import org.junit.Test

class RedactorTest{

    @Test
    fun redact_masksEmailsAndPhones(){
        val redactor= Redactor()
        val input=  "Contact user at test@test.com or call +213 555 123 456."
        val result= redactor.redact(input)

        assertEquals("Contact user at [REDACTED] or call [REDACTED].",result)
    }

    @Test
    fun redact_denyListOverridesAllowList(){
        val redactor =Redactor(
            denyList = listOf("support@mseismo.com"),
            allowList=listOf("support@mseismo.com")
            )
        val result=redactor.redact(
            "contact support@mseismo.com for help"
        )

        assertEquals(
            "contact [REDACTED] for help",
            result
        )
    }

}