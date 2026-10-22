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
    fun redact_respectAllowList(){
        val redactor =Redactor(
            allowList=listOf("support@mseismo.com")
            )
        val input="Email support@mseismo.com or user@example.com"
        val result=redactor.redact(input)

        assertEquals("Email support@mseismo.com or [REDACTED]",result)
    }

}