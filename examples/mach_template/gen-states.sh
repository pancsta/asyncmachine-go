cd states
am-gen states-file \
  --states 'ErrExample:require(Exception),Foo:require(Bar),Bar,Baz:multi,BazDone:multi,Channel' \
  --inherit basic,connected,disposed \
  --name machTemplate \
  --groups Group1,Group2 \
  --force
