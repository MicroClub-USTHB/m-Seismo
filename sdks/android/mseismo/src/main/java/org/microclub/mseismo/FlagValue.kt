package org.microclub.mseismo

public sealed class FlagValue {
    public data class StringValue(val value: String) : FlagValue()
    public data class NumberValue(val value: Double) : FlagValue()
    public data class BooleanValue(val value: Boolean) : FlagValue()
}
