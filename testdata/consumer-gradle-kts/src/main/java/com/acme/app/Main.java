package com.acme.app;

import com.acme.LibUtils;

public final class Main {
    public static void main(String[] args) {
        if (LibUtils.answer() != 42) {
            throw new IllegalStateException("unexpected answer");
        }
    }
}
