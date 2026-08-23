using Acme;

if (LibUtils.Answer() != 42) {
    throw new System.Exception("unexpected answer");
}
